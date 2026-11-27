package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"ovpn/internal/model"
	"ovpn/internal/operations"
	"ovpn/internal/subscription"
)

const (
	createSubscription  = "user.subscription.create"
	disableSubscription = "user.subscription.disable"
)

func subscriptionUserAction(action string) string {
	switch action {
	case createSubscription:
		return "user.add"
	case disableSubscription:
		return "user.disable"
	}
	return action
}

func hasSubscriptionUser(users []model.User, id int64, username string) bool {
	for _, user := range users {
		if user.ServerID == id && user.Username == username {
			return true
		}
	}
	return false
}

func (a *App) refreshManagedAfterOperation() {
	if err := a.syncManagedSubscriptions(); err != nil {
		a.log().Warn("managed subscription refresh will be retried", "code", "SUBSCRIPTION_SYNC_PENDING")
	}
}

func (d *deskControl) watchManagedSubscriptions(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := os.Stat(filepath.Join(d.dataDir, "delivery", "bundles")); err != nil {
				continue
			}
			if err := operations.WithWriter(d.dataDir, func() error {
				a, err := d.instance(ctx, operations.Target{ID: "workspace"}, "subscription.sync")
				if err != nil {
					return err
				}
				defer a.store.Close()
				return a.syncManagedSubscriptions()
			}); err != nil && ctx.Err() == nil && !errors.Is(err, operations.ErrBusy) {
				d.template.log().Warn("managed subscription refresh is pending", "code", "SUBSCRIPTION_SYNC_PENDING")
			}
		}
	}
}

func (a *App) managedBundleDir() string { return filepath.Join(a.dataDir, "delivery", "bundles") }
func (a *App) managedInviteDir() string { return filepath.Join(a.dataDir, "delivery", "invites") }

func subscriptionIdentity(uuid string) string {
	key := sha256.Sum256([]byte(uuid))
	return hex.EncodeToString(key[:])
}

type subscriptionNode struct {
	server model.Server
	users  map[string]bool
	known  bool
}

func (a *App) loadSubscriptionNodes() ([]subscriptionNode, error) {
	servers, err := a.store.SubscriptionNodes(a.ctx)
	if err != nil {
		return nil, err
	}
	var nodes []subscriptionNode
	for _, applied := range servers {
		node := subscriptionNode{server: applied}
		live, err := a.store.GetServerByID(a.ctx, applied.ID)
		if err != nil {
			return nil, err
		}
		raw, err := a.fetchRemoteAgent(*live, "GET", a.agentURL("/users/status"), nil)
		var report model.UserStatusResponse
		if err == nil && json.Unmarshal(raw, &report) == nil && (report.Time != "" || report.Users != nil) {
			node.known = true
			node.users = make(map[string]bool, len(report.Users))
			for _, user := range report.Users {
				if user.EffectiveEnabled && user.HasRuntimeIdentity {
					node.users[user.Username+"\x00"+subscriptionIdentity(user.UUID)] = true
				}
			}
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

func (a *App) managedEntries(bundle subscription.Bundle, nodes []subscriptionNode, labels subscriptionLabels) ([]subscription.Entry, error) {
	var entries []subscription.Entry
	for _, node := range nodes {
		srv := node.server
		user, err := a.store.GetUser(a.ctx, srv.ID, bundle.Owner)
		if isNotFoundErr(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if subscriptionIdentity(user.UUID) != bundle.ManagedKey || !effectiveUserEnabled(*user) {
			continue
		}
		if !node.known {
			// A transient management failure must not remove any last published route
			// for an otherwise eligible user. Never add a route without runtime evidence.
			for _, entry := range bundle.Entries {
				if entry.NodeID == srv.ID {
					entries = append(entries, entry)
				}
			}
			continue
		}
		if !node.users[bundle.Owner+"\x00"+bundle.ManagedKey] {
			continue
		}
		if validateClientCheckUser(srv, *user) != nil {
			continue
		}
		for _, profile := range deployableEnabledProfiles(srv) {
			link, err := buildUserProfileLink(srv, *user, profile)
			if err != nil {
				return nil, err
			}
			parsed, err := url.Parse(link)
			if err != nil {
				return nil, err
			}
			parsed.Fragment = labels.fragment(srv.Name, profile)
			entries = append(entries, subscription.Entry{Link: parsed.String(), ExpiresAt: user.ExpiryDate, NodeID: srv.ID})
		}
	}
	return entries, nil
}

func (a *App) syncManagedSubscriptions() error {
	store, err := subscription.Open(a.managedBundleDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer store.Close()
	bundles, err := store.Managed()
	if err != nil || len(bundles) == 0 {
		return err
	}
	labels, err := a.readSubscriptionLabels()
	if err != nil {
		return err
	}
	nodes, err := a.loadSubscriptionNodes()
	if err != nil {
		return err
	}
	var failures error
	for _, bundle := range bundles {
		if bundle.Disabled {
			continue
		}
		entries, err := a.managedEntries(bundle, nodes, labels)
		if err == nil && !reflect.DeepEqual(entries, bundle.Entries) {
			_, _, err = store.PublishManaged(bundle.ID, bundle.Owner, bundle.ManagedKey, entries, false, false)
		}
		failures = errors.Join(failures, err)
	}
	for _, node := range nodes {
		if !node.known {
			failures = errors.Join(failures, errors.New("SUBSCRIPTION_NODE_UNAVAILABLE"))
		}
	}
	return failures
}

func (a *App) newSubscriptionSyncCmd() *cobra.Command {
	return &cobra.Command{Use: "sync", Short: "Refresh managed subscriptions from verified server state", Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			if a.dryRun {
				return errors.New("subscription sync does not support --dry-run")
			}
			return a.syncManagedSubscriptions()
		},
	}
}

func (a *App) newServerSubscriptionCmd() *cobra.Command {
	var publish bool
	cmd := &cobra.Command{Use: "subscription <server>", Short: "Include a server in subscriptions after a verified deploy", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if a.dryRun {
				return errors.New("server subscription does not support --dry-run")
			}
			srv, err := a.store.GetServerByName(a.ctx, args[0])
			if err != nil {
				return err
			}
			if err = a.store.SetSubscriptionNode(a.ctx, srv.ID, publish); err != nil {
				return err
			}
			if publish {
				fmt.Fprintln(a.output(), "server will publish after a verified deploy")
			}
			return a.syncManagedSubscriptions()
		},
	}
	cmd.Flags().BoolVar(&publish, "publish", true, "Show this server as a public connection option")
	return cmd
}

func (a *App) newManagedSubscriptionCmd(create bool) *cobra.Command {
	var username, origin, format, expiry, uuid string
	var quota int64
	name, title := "issue", "Issue subscription access to an existing VPN user"
	if create {
		name, title = "create", "Create subscription"
	}
	cmd := &cobra.Command{Use: name, Short: title, Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if a.dryRun {
				return errors.New("issuing a subscription does not support --dry-run")
			}
			if a.operationTarget != 0 || !a.internal {
				return errors.New("managed subscriptions require the operation coordinator")
			}
			return a.issueManagedSubscription(strings.TrimSpace(username), uuid, origin, format, create)
		},
	}
	cmd.Flags().StringVar(&username, "username", "", "Username")
	cmd.Flags().StringVar(&origin, "base-url", "", "HTTPS address of the subscription service")
	cmd.Flags().StringVar(&format, "format", "code", "Delivery method: code or link")
	_ = cmd.MarkFlagRequired("username")
	_ = cmd.MarkFlagRequired("base-url")
	if create {
		cmd.Flags().StringVar(&uuid, "uuid", "", "Operation identity")
		_ = cmd.Flags().MarkHidden("uuid")
		cmd.Flags().StringVar(&expiry, "expiry", "", "Expiry date YYYY-MM-DD")
		cmd.Flags().Int64Var(&quota, "quota-bytes", 0, "Rolling 30-day traffic quota per server in bytes")
	}
	return cmd
}

func (d *deskControl) BeforeApply(ctx context.Context, plan operations.Plan) error {
	if plan.Action != createSubscription {
		return nil
	}
	a, err := d.instance(ctx, operations.Target{ID: "workspace"}, plan.Action)
	if err != nil {
		return err
	}
	defer a.store.Close()
	if _, err := a.readSubscriptionLabels(); err != nil {
		return err
	}
	if err := verifyManagedDelivery(ctx, plan.Request.Values["base-url"], plan.Request.Values["format"]); err != nil {
		return err
	}
	nodes, err := a.loadSubscriptionNodes()
	if err != nil {
		return err
	}
	for _, node := range nodes {
		if node.known && len(deployableEnabledProfiles(node.server)) > 0 {
			return nil
		}
	}
	return errors.New("SUBSCRIPTION_NO_READY_PROFILES")
}

func verifyManagedDelivery(ctx context.Context, origin, format string) error {
	switch format {
	case "", "code":
		return subscription.VerifyActivation(ctx, origin)
	case "link":
		return subscription.VerifySubscriptionRoute(ctx, origin)
	default:
		return errors.New("DELIVERY_UNAVAILABLE")
	}
}

func (a *App) issueManagedSubscription(username, uuid, origin, format string, create bool) error {
	labels, err := a.readSubscriptionLabels()
	if err != nil {
		return err
	}
	if err := verifyManagedDelivery(a.ctx, origin, format); err != nil {
		return err
	}
	if !create {
		var err error
		uuid, err = a.subscriptionUserIdentity(username)
		if err != nil {
			return err
		}
	}
	for _, dir := range []string{a.managedBundleDir(), a.managedInviteDir()} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	store, err := subscription.Open(a.managedBundleDir())
	if err != nil {
		return err
	}
	defer store.Close()
	bundles, err := store.Managed()
	if err != nil {
		return err
	}
	bundle := subscription.Bundle{Owner: username, ManagedKey: subscriptionIdentity(uuid)}
	for _, existing := range bundles {
		if existing.Owner == username && !existing.Disabled && existing.ManagedKey == bundle.ManagedKey {
			bundle = existing
			break
		}
	}
	nodes, err := a.loadSubscriptionNodes()
	if err != nil {
		return err
	}
	entries, err := a.managedEntries(bundle, nodes, labels)
	if err != nil {
		return err
	}
	encoded, err := subscription.Encode(entries, time.Now(), "base64")
	if err != nil {
		return errors.New("SUBSCRIPTION_NO_READY_PROFILES")
	}
	id, token, err := store.PublishManaged(bundle.ID, username, bundle.ManagedKey, entries, format == "link", false)
	if err != nil {
		return err
	}

	if token != "" {
		link, err := subscription.URL(origin, id, token)
		if err != nil {
			return err
		}
		if err = subscription.VerifyDelivery(a.ctx, origin, link, encoded); err != nil {
			if bundle.ID == "" {
				return errors.Join(err, store.Revoke(id))
			}
			if rollback := store.RestoreManagedRotation(bundle, token); rollback != nil {
				return errors.Join(err, rollback)
			}
			return err
		}
		if format == "link" {
			a.sharedContent = link
		}
	}
	if format == "code" {
		published, err := store.Read(id)
		if err != nil {
			return err
		}
		invites, err := subscription.OpenInvites(a.managedInviteDir())
		if err != nil {
			return err
		}
		defer invites.Close()
		a.sharedContent, err = invites.Issue(published, origin, time.Now(), 30*time.Minute)
		if err != nil {
			return err
		}
	}
	a.sharedData, _ = json.Marshal(map[string]string{"subscriptionId": id})
	fmt.Fprintf(a.output(), "subscription id: %s\nprofiles: %d\n%s\n", id, len(entries), a.sharedContent)
	return nil
}

func (a *App) subscriptionUserIdentity(username string) (string, error) {
	servers, err := a.listEnabledServers()
	if err != nil {
		return "", err
	}
	var identity *model.User
	for _, server := range servers {
		user, err := a.store.GetUser(a.ctx, server.ID, username)
		if isNotFoundErr(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if identity != nil && (user.UUID != identity.UUID || user.Email != identity.Email) {
			return "", errors.New("IDENTITY_CONFLICT")
		}
		identity = user
	}
	if identity == nil {
		return "", errors.New("SUBSCRIPTION_USER_NOT_FOUND")
	}
	return identity.UUID, nil
}

func (a *App) newManagedSubscriptionDisableCmd() *cobra.Command {
	var username string
	cmd := &cobra.Command{Use: "disable", Short: "Disable subscription delivery and VPN access", Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			if a.dryRun || !a.internal {
				return errors.New("disable subscription requires the operation coordinator")
			}
			store, err := subscription.Open(a.managedBundleDir())
			if err != nil {
				return err
			}
			defer store.Close()
			bundles, err := store.Managed()
			if err != nil {
				return err
			}
			for _, bundle := range bundles {
				if bundle.Owner == username && !bundle.Disabled {
					if _, _, err := store.PublishManaged(bundle.ID, bundle.Owner, bundle.ManagedKey, nil, false, true); err != nil {
						return err
					}
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&username, "username", "", "Username")
	_ = cmd.MarkFlagRequired("username")
	return cmd
}

func (a *App) newSubscriptionListCmd() *cobra.Command {
	var username string
	cmd := &cobra.Command{Use: "list", Short: "Show managed subscriptions and their support IDs", Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			store, err := subscription.Open(a.managedBundleDir())
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			if err != nil {
				return err
			}
			defer store.Close()
			bundles, err := store.Managed()
			if err != nil {
				return err
			}
			for _, bundle := range bundles {
				if username != "" && bundle.Owner != username {
					continue
				}
				state := "active"
				if bundle.Disabled {
					state = "disabled"
				} else if len(bundle.Entries) == 0 {
					state = "no available profiles"
				}
				revision := "—"
				if profiles, err := subscription.Encode(bundle.Entries, time.Now(), "base64"); err == nil {
					revision = subscription.ProfileRevision(string(profiles))
				}
				updated := "—"
				if bundle.UpdatedAt != nil {
					updated = bundle.UpdatedAt.Format(time.RFC3339)
				}
				fmt.Fprintf(a.output(), "%s  %s  %s  %d profiles  %s  %s\n", bundle.Owner, bundle.ID, state, len(bundle.Entries), revision, updated)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&username, "username", "", "Filter by username")
	return cmd
}
