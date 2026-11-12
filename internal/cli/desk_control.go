package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/skip2/go-qrcode"

	"ovpn/internal/backup"
	"ovpn/internal/ha"
	"ovpn/internal/haproxy"
	"ovpn/internal/logx"
	"ovpn/internal/model"
	"ovpn/internal/operations"
	"ovpn/internal/store/local"
	"ovpn/internal/util"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

type deskControl struct {
	dataDir     string
	interactive bool
	template    *App
}
type controlSnapshot struct {
	Servers  []model.Server       `json:"servers"`
	Users    []model.User         `json:"users"`
	Backends []model.ProxyBackend `json:"backends"`
}

func (d *deskControl) Catalog() []operations.Action { return commandCatalog(newRootCmd(&App{})) }
func (d *deskControl) snapshot(ctx context.Context) (controlSnapshot, string, error) {
	snap := controlSnapshot{Servers: []model.Server{}, Users: []model.User{}, Backends: []model.ProxyBackend{}}
	st, err := local.OpenReadOnly(ctx, d.dataDir)
	if errors.Is(err, os.ErrNotExist) {
		sum := sha256.Sum256([]byte("empty"))
		return snap, hex.EncodeToString(sum[:]), nil
	}
	if err != nil {
		return snap, "", operations.ErrUnavailable
	}
	defer st.Close()
	snap.Servers, err = st.ListServers(ctx)
	if err != nil {
		return snap, "", err
	}
	for _, server := range snap.Servers {
		rows, err := st.ListUsers(ctx, server.ID)
		if err != nil {
			return snap, "", err
		}
		snap.Users = append(snap.Users, rows...)
		backends, err := st.ListProxyBackends(ctx, server.ID)
		if err != nil {
			return snap, "", err
		}
		snap.Backends = append(snap.Backends, backends...)
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		return snap, "", err
	}
	sum := sha256.Sum256(raw)
	return snap, hex.EncodeToString(sum[:]), nil
}
func (d *deskControl) Revision(ctx context.Context) (string, error) {
	_, revision, err := d.snapshot(ctx)
	return revision, err
}

func (d *deskControl) Prepare(ctx context.Context, request operations.Request) (operations.Plan, error) {
	var definition *operations.Action
	for _, a := range d.Catalog() {
		if a.ID == request.Action {
			copy := a
			definition = &copy
			break
		}
	}
	if definition == nil {
		return operations.Plan{}, operations.ErrInvalid
	}
	if request.Values == nil {
		request.Values = map[string]string{}
	}
	if d.interactive && request.Action == "subscription.invite" {
		request.Values["check-service"] = "true"
	}
	if request.Action == "user.link" {
		request.Values["qr"] = "false"
	}
	if request.Action == "user.add" && request.Values["uuid"] == "" {
		request.Values["uuid"] = uuid.NewString()
	}
	instance := &App{internal: true, ctx: ctx, dataDir: d.dataDir, writer: io.Discard}
	if _, err := prepareInvocation(instance, request); err != nil {
		return operations.Plan{}, operations.ErrInvalid
	}
	snap, revision, err := d.snapshot(ctx)
	if err != nil {
		return operations.Plan{}, operations.ErrUnavailable
	}
	targets := []operations.Target{}
	global := userGlobalMutation(request.Action)
	name := request.Values["server"]
	if name == "" && len(request.Arguments) > 0 {
		name = request.Arguments[0]
	}
	if request.Values["proxy"] != "" {
		name = request.Values["proxy"]
	}
	stopOnFailure := false
	mutation := definition.Mutation
	if request.Action == "user.reconcile" {
		source := strings.TrimSpace(request.Values["from-server"])
		destination := strings.TrimSpace(request.Values["to-server"])
		all, _ := strconv.ParseBool(request.Values["all"])
		mutation, _ = strconv.ParseBool(request.Values["apply"])
		found := false
		for _, server := range snap.Servers {
			if server.Name == source {
				found = true
			}
		}
		if !found || source == destination || (destination != "" && all) {
			return operations.Plan{}, operations.ErrInvalid
		}
		for _, server := range snap.Servers {
			if server.Name != source && ((destination != "" && server.Name == destination) || (destination == "" && (all || server.Enabled))) {
				targets = append(targets, operations.Target{ID: fmt.Sprintf("node-%d", server.ID), Name: server.Name})
			}
		}
		if len(targets) == 0 {
			return operations.Plan{}, operations.ErrInvalid
		}
	}
	for _, server := range snap.Servers {
		if request.Action != "user.reconcile" && ((global && server.Enabled) || (!global && name == server.Name)) {
			targets = append(targets, operations.Target{ID: fmt.Sprintf("node-%d", server.ID), Name: server.Name})
		}
	}

	if request.Action == "server.ha-apply" {
		st, e := local.OpenReadOnly(ctx, d.dataDir)
		if e != nil {
			return operations.Plan{}, operations.ErrUnavailable
		}
		defer st.Close()
		proxy, e := st.GetServerByName(ctx, name)
		if e != nil || !proxy.IsProxy() || !proxy.Enabled {
			return operations.Plan{}, operations.ErrInvalid
		}

		backends, e := st.ListAttachedBackendServers(ctx, proxy.ID)
		if e != nil {
			return operations.Plan{}, operations.ErrUnavailable
		}
		nodes, e := ha.Plan(*proxy, backends)
		if e != nil {
			return operations.Plan{}, e
		}
		targets = nil
		for _, node := range nodes {
			targets = append(targets, operations.Target{ID: fmt.Sprintf("node-%d", node.ID), Name: node.Name})
		}
		stopOnFailure = true
	}
	if global && len(targets) == 0 {
		return operations.Plan{}, errors.New("NO_ENABLED_SERVERS")
	}
	if global {
		validationStore, e := local.OpenReadOnly(ctx, d.dataDir)
		if e != nil {
			return operations.Plan{}, operations.ErrUnavailable
		}
		validationApp := &App{ctx: ctx, store: validationStore}
		_, e = validationApp.resolveUserMutationServers()
		_ = validationStore.Close()
		if e != nil {
			return operations.Plan{}, errors.New("IDENTITY_CONFLICT")
		}
		username := strings.TrimSpace(request.Values["username"])
		if username == "" {
			return operations.Plan{}, operations.ErrInvalid
		}
		for _, target := range targets {
			exists := false
			for _, u := range snap.Users {
				if target.ID == fmt.Sprintf("node-%d", u.ServerID) && u.Username == username {
					exists = true
					break
				}
			}
			if (request.Action == "user.add" && exists) || (request.Action != "user.add" && !exists) {
				return operations.Plan{}, errors.New("USER_ASSIGNMENT_CONFLICT")
			}
		}
		if value := request.Values["uuid"]; value != "" {
			if _, e := uuid.Parse(value); e != nil {
				return operations.Plan{}, operations.ErrInvalid
			}
		}
		for _, field := range []string{"expiry", "date"} {
			if _, e := model.ParseExpiryDate(request.Values[field]); e != nil {
				return operations.Plan{}, operations.ErrInvalid
			}
		}
		for _, field := range []string{"quota-bytes", "monthly-bytes", "monthly-gb"} {
			if value := request.Values[field]; value != "" {
				n, e := strconv.ParseInt(value, 10, 64)
				if e != nil || n < 0 {
					return operations.Plan{}, operations.ErrInvalid
				}
			}
		}
		seenUUID := ""
		for _, user := range snap.Users {
			if user.Username == request.Values["username"] {
				if seenUUID != "" && seenUUID != user.UUID {
					return operations.Plan{}, errors.New("IDENTITY_CONFLICT")
				}
				seenUUID = user.UUID
			}
		}
	}
	if len(targets) == 0 {
		targets = append(targets, operations.Target{ID: "workspace", Name: "Local workspace"})
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		return operations.Plan{}, err
	}
	return operations.Plan{Action: definition.ID, Title: definition.Title, StopOnFailure: stopOnFailure, Mutation: mutation, Targets: targets, Destructive: definition.Destructive && mutation, Request: request, Revision: revision, Snapshot: raw}, nil
}
func userGlobalMutation(action string) bool {
	switch action {
	case "user.add", "user.rm", "user.enable", "user.disable", "user.expiry-set", "user.expiry-clear", "user.quota-set", "user.quota-reset":
		return true
	}
	return false
}

func prepareInvocation(a *App, request operations.Request) (*cobra.Command, error) {
	root := newRootCmd(a)
	path := strings.Split(request.Action, ".")
	command, remaining, err := root.Find(path)
	if err != nil || len(remaining) != 0 || command.RunE == nil || actionID(command) != request.Action || command.Name() == "machine" {
		return nil, operations.ErrInvalid
	}
	for name, value := range request.Values {
		flag := command.Flags().Lookup(name)
		if flag == nil || name == "help" {
			return nil, operations.ErrInvalid
		}
		if err := flag.Value.Set(value); err != nil {
			return nil, operations.ErrInvalid
		}
		flag.Changed = true
	}
	command.SetContext(a.ctx)
	command.SetOut(a.output())
	command.SetErr(a.output())
	if err := command.ValidateRequiredFlags(); err != nil {
		return nil, operations.ErrInvalid
	}
	if command.Args != nil {
		if err := command.Args(command, request.Arguments); err != nil {
			return nil, operations.ErrInvalid
		}
	}
	return command, nil
}

func (d *deskControl) instance(ctx context.Context, target operations.Target, action string) (*App, error) {
	a := &App{internal: true, ctx: ctx, dataDir: d.dataDir, writer: io.Discard, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	var err error
	root := newRootCmd(a)
	selected, _, findErr := root.Find(strings.Split(action, "."))
	if findErr != nil {
		return nil, operations.ErrInvalid
	}
	if selected.Annotations["ovpn.local-state"] != "none" {
		a.store, err = local.Open(ctx, d.dataDir)
		if err != nil {
			return nil, err
		}
		cwd, _ := os.Getwd()
		a.repoRoot, err = resolveRepoRoot(cwd)
		if err != nil {
			a.store.Close()
			return nil, err
		}
	}
	if d.template != nil {
		a.remoteHTTPHook = d.template.remoteHTTPHook
		a.remoteExecHook = d.template.remoteExecHook
		a.sleepHook = d.template.sleepHook
	}
	if strings.HasPrefix(target.ID, "node-") {
		a.operationTarget, _ = strconv.ParseInt(strings.TrimPrefix(target.ID, "node-"), 10, 64)
	}
	return a, nil
}

func (d *deskControl) Backup(ctx context.Context, p operations.Plan, target operations.Target) error {
	if !p.Mutation || p.Action == "server.backup" || strings.HasPrefix(p.Action, "subscription.") || strings.HasPrefix(p.Action, "observe.") {
		return nil
	}
	if !remoteMutation(p.Action) {
		if p.Action == "user.reconcile" || p.Action == "server.add" || strings.HasPrefix(p.Action, "server.profile.") || strings.HasPrefix(p.Action, "server.backend.") || strings.HasPrefix(p.Action, "server.set-") {
			_, _, err := backup.LocalBackup(d.dataDir, filepath.Join(d.dataDir, "backups"))
			return err
		}
		return nil
	}
	a, err := d.instance(ctx, target, p.Action)
	if err != nil {
		return err
	}
	defer func() {
		if a.store != nil {
			_ = a.store.Close()
		}
	}()
	server, err := a.store.GetServerByID(ctx, a.operationTarget)
	if err != nil {
		return err
	}
	// A registered, uninitialised node has no remote runtime to back up.
	if server.LastDeployAt == nil && (p.Action == "server.init" || p.Action == "server.ha-apply") {
		state, e := a.newRunner("server.detect").Exec(ctx, sshFromServer(*server), "if sudo -n test -d /opt/ovpn; then printf present; else printf absent; fi")
		if e != nil {
			return e
		}
		if strings.TrimSpace(state.Stdout) == "absent" {
			_, _, e := backup.LocalBackup(d.dataDir, filepath.Join(d.dataDir, "backups"))
			return e
		}
		if strings.TrimSpace(state.Stdout) != "present" {
			return errors.New("BACKUP_FAILED")
		}
	}

	cmd := a.newServerBackupCmd()
	if err := cmd.RunE(cmd, []string{server.Name}); err != nil {
		return err
	}
	records, err := a.store.ListBackupRecords(ctx, server.ID)
	if err != nil || len(records) == 0 {
		return errors.New("BACKUP_FAILED")
	}
	record := records[len(records)-1]
	for _, candidate := range records {
		if candidate.CreatedAt.After(record.CreatedAt) {
			record = candidate
		}
	}
	sum, err := util.SHA256File(record.Path)
	if err != nil || sum != record.SHA256 {
		return errors.New("BACKUP_FAILED")
	}
	return nil
}

type boundedOutput struct{ bytes.Buffer }

func (b *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := 64*1024 - b.Len()
	if remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		_, _ = b.Buffer.Write(p)
	}
	return n, nil
}

func (d *deskControl) Apply(ctx context.Context, p operations.Plan, target operations.Target, recovering bool) (operations.Result, error) {
	a, err := d.instance(ctx, target, p.Action)
	if err != nil {
		return operations.Result{}, err
	}
	defer func() {
		if a.store != nil {
			_ = a.store.Close()
		}
	}()
	output := &boundedOutput{}
	a.writer = output
	request := p.Request
	if request.Action == "user.reconcile" {
		request.Values = maps.Clone(request.Values)
		request.Values["to-server"] = target.Name
		request.Values["all"] = "false"
	}
	cmd, err := prepareInvocation(a, request)
	if err != nil {
		return operations.Result{}, err
	}
	if recovering && p.Mutation && !userGlobalMutation(request.Action) {
		return operations.Result{}, operations.ErrStale
	}
	if recovering && userGlobalMutation(request.Action) {
		if e := d.validateUserRecovery(ctx, p); e != nil {
			return operations.Result{}, e
		}
		result, done, err := a.recoverUserOperation(p, target)
		if done {
			return result, err
		}
	}
	err = cmd.RunE(cmd, request.Arguments)
	text := strings.TrimSpace(output.String())
	result := operations.Result{Text: logx.Sanitize(text), Data: a.sharedData}
	if request.Action == "user.invite" || request.Action == "user.link" || request.Action == "subscription.invite" || request.Action == "user.subscription.publish" {
		metadata := strings.TrimSpace(strings.ReplaceAll(text, a.sharedContent, ""))
		result = operations.Result{Text: logx.Sanitize(metadata), Content: a.sharedContent, Data: a.sharedData, MediaType: "text/plain"}
		if a.sharedContent != "" {
			if png, e := qrcode.Encode(a.sharedContent, qrcode.Medium, 384); e == nil {
				result.QRData = "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
			}
		}
	}
	if err != nil {
		return result, err
	}
	switch request.Action {
	case "server.ha-apply", "server.init", "deploy", "restart", "server.restore":
		if err := a.verifyServerOperation(); err != nil {
			return result, err
		}
	}
	if request.Action == "server.ha-apply" {
		if err := a.verifyHAOperation(); err != nil {
			return result, err
		}
	}
	if userGlobalMutation(request.Action) {
		if err := a.verifyUserOperation(p); err != nil {
			return result, err
		}
	}
	return result, nil
}

func (a *App) recoverUserOperation(p operations.Plan, _ operations.Target) (operations.Result, bool, error) {
	username := p.Request.Values["username"]
	user, err := a.store.GetUser(a.ctx, a.operationTarget, username)
	if p.Action == "user.add" && err == nil {
		if user.UUID != p.Request.Values["uuid"] {
			return operations.Result{}, true, errors.New("IDENTITY_CONFLICT")
		}
		server, err := a.store.GetServerByID(a.ctx, a.operationTarget)
		if err != nil {
			return operations.Result{}, true, err
		}
		if err = a.applyRuntimeUser(*server, *user, effectiveUserEnabled(*user)); err != nil {
			return operations.Result{}, true, err
		}
		if err = a.persistAndSyncPolicies(*server); err != nil {
			return operations.Result{}, true, err
		}
		return operations.Result{}, true, a.verifyUserOperation(p)
	}
	if p.Action == "user.rm" && isNotFoundErr(err) {
		var snap controlSnapshot
		if json.Unmarshal(p.Snapshot, &snap) != nil {
			return operations.Result{}, true, operations.ErrUnavailable
		}
		for _, before := range snap.Users {
			if before.ServerID == a.operationTarget && before.Username == username {
				server, e := a.store.GetServerByID(a.ctx, a.operationTarget)
				if e != nil {
					return operations.Result{}, true, e
				}
				e = a.applyRuntimeUser(*server, before, false)
				if e == nil {
					e = a.persistAndSyncPolicies(*server)
				}
				if e == nil {
					e = a.verifyUserOperation(p)
				}
				return operations.Result{}, true, e
			}
		}
	}
	return operations.Result{}, false, nil
}

func (a *App) verifyUserOperation(p operations.Plan) error {
	server, err := a.store.GetServerByID(a.ctx, a.operationTarget)
	if err != nil {
		return err
	}
	raw, err := a.fetchRemoteAgent(*server, "GET", a.agentURL("/users/status"), nil)
	if err != nil {
		return err
	}
	var status model.UserStatusResponse
	if json.Unmarshal(raw, &status) != nil {
		return errors.New("APPLY_UNVERIFIED")
	}
	username := p.Request.Values["username"]
	for _, u := range status.Users {
		if u.Username == username {
			if p.Action == "user.rm" {
				if u.HasRuntimeIdentity || u.EffectiveEnabled {
					return errors.New("APPLY_UNVERIFIED")
				}
				return nil
			}
			localUser, err := a.store.GetUser(a.ctx, server.ID, username)
			if err != nil {
				return err
			}
			effective := effectiveUserEnabled(*localUser)
			quotaMismatch := effective && (u.QuotaEnabled != localUser.QuotaEnabled || (localUser.TrafficLimitByte != nil && u.Window30DQuotaByte != *localUser.TrafficLimitByte))
			if u.UUID != localUser.UUID || u.Email != localUser.Email || u.Enabled != localUser.Enabled || u.EffectiveEnabled != effective || u.ExpiryDate != model.ExpiryDateString(localUser.ExpiryDate) || quotaMismatch || (!effective && u.HasRuntimeIdentity) || (effective && !u.HasRuntimeIdentity && !u.BlockedByQuota) {
				return errors.New("APPLY_UNVERIFIED")
			}
			return nil
		}
	}
	if p.Action == "user.rm" {
		return nil
	}
	return errors.New("APPLY_UNVERIFIED")
}

func (d *deskControl) Lock(ctx context.Context, p operations.Plan, target operations.Target) (context.Context, func(), error) {
	if target.ID == "workspace" || !remoteMutation(p.Action) {
		return ctx, func() {}, nil
	}
	a, err := d.instance(ctx, target, p.Action)
	if err != nil {
		return nil, nil, err
	}
	defer a.store.Close()
	server, err := a.store.GetServerByID(ctx, a.operationTarget)
	if err != nil {
		return nil, nil, err
	}
	return a.newRunner("operation.lock").Lock(ctx, sshFromServer(*server))
}

// Recovery can finish the reviewed user's intent, but cannot overwrite unrelated
// local changes made after the interrupted write.
func (d *deskControl) validateUserRecovery(ctx context.Context, p operations.Plan) error {
	var before controlSnapshot
	if json.Unmarshal(p.Snapshot, &before) != nil {
		return operations.ErrStale
	}
	current, _, err := d.snapshot(ctx)
	if err != nil {
		return err
	}
	// A successful fallback deploy records execution timestamps on a reviewed node.
	// Desired server configuration must still match the captured intent.
	for i := range current.Servers {
		for _, original := range before.Servers {
			if original.ID != current.Servers[i].ID {
				continue
			}
			for _, target := range p.Targets {
				if target.ID == fmt.Sprintf("node-%d", original.ID) {
					current.Servers[i].UpdatedAt = original.UpdatedAt
					current.Servers[i].LastDeployAt = original.LastDeployAt
				}
			}
		}
	}
	left, _ := json.Marshal(before.Servers)
	right, _ := json.Marshal(current.Servers)
	if !bytes.Equal(left, right) {
		return operations.ErrStale
	}
	left, _ = json.Marshal(before.Backends)
	right, _ = json.Marshal(current.Backends)
	if !bytes.Equal(left, right) {
		return operations.ErrStale
	}
	key := func(u model.User) string { return fmt.Sprintf("%d/%s", u.ServerID, u.Username) }
	old := map[string]model.User{}
	for _, u := range before.Users {
		old[key(u)] = u
	}
	touched := func(u model.User) bool {
		if u.Username != p.Request.Values["username"] {
			return false
		}
		for _, t := range p.Targets {
			if t.ID == fmt.Sprintf("node-%d", u.ServerID) {
				return true
			}
		}
		return false
	}
	for _, u := range current.Users {
		previous, exists := old[key(u)]
		delete(old, key(u))
		if !exists {
			if p.Action != "user.add" || !touched(u) || u.UUID != p.Request.Values["uuid"] {
				return operations.ErrStale
			}
			continue
		}
		if touched(u) {
			u.UpdatedAt = previous.UpdatedAt
			switch p.Action {
			case "user.enable", "user.disable":
				u.Enabled = previous.Enabled
			case "user.expiry-set", "user.expiry-clear":
				u.Enabled = previous.Enabled
				u.ExpiryDate = previous.ExpiryDate
			case "user.quota-set", "user.quota-reset":
				u.TrafficLimitByte = previous.TrafficLimitByte
				u.QuotaEnabled = previous.QuotaEnabled
				u.QuotaBlocked = previous.QuotaBlocked
				u.QuotaBlockedAt = previous.QuotaBlockedAt
			}
		}
		left, _ := json.Marshal(previous)
		right, _ := json.Marshal(u)
		if !bytes.Equal(left, right) {
			return operations.ErrStale
		}
	}
	for _, u := range old {
		if p.Action != "user.rm" || !touched(u) {
			return operations.ErrStale
		}
	}
	return nil
}

func (d *deskControl) CanResume(ctx context.Context, p operations.Plan) error {
	if !p.Mutation {
		return nil
	}
	if !userGlobalMutation(p.Action) {
		return operations.ErrStale
	}
	return d.validateUserRecovery(ctx, p)
}

func remoteMutation(action string) bool {
	if userGlobalMutation(action) {
		return true
	}
	switch action {
	case "server.ha-apply", "deploy", "restart", "server.init", "server.restore", "server.cleanup", "server.backup", "server.monitor.up", "server.monitor.down", "server.monitor.telegram-setup", "user.debug.start", "user.debug.stop":
		return true
	}
	return false
}

func (a *App) verifyServerOperation() error {
	ctx, cancel := context.WithTimeout(a.ctx, 45*time.Second)
	defer cancel()
	previous := a.ctx
	a.ctx = ctx
	defer func() { a.ctx = previous }()
	server, err := a.store.GetServerByID(ctx, a.operationTarget)
	if err != nil {
		return err
	}
	for {
		raw, e := a.fetchRemoteAgent(*server, "GET", a.agentURL("/health"), nil)
		if e == nil {
			var health agentHealth
			if json.Unmarshal(raw, &health) == nil && health.XrayAPIReachable != nil && *health.XrayAPIReachable {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return errors.New("APPLY_UNVERIFIED")
		case <-time.After(time.Second):
		}
	}
}

func (a *App) verifyHAOperation() error {
	server, err := a.store.GetServerByID(a.ctx, a.operationTarget)
	if err != nil {
		return err
	}
	if !server.IsProxy() {
		return nil
	}
	backends, err := a.attachedBackendServers(*server)
	if err != nil || len(backends) == 0 {
		return errors.New("APPLY_UNVERIFIED")
	}
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	defer cancel()
	previous := a.ctx
	a.ctx = ctx
	defer func() { a.ctx = previous }()
	for {
		body, e := a.fetchRemoteAgent(*server, "GET", a.agentURL("/ha/status"), nil)
		var status haproxy.Status
		if e == nil && json.Unmarshal(body, &status) == nil && status.Supported && status.Code == "" && len(status.Backends) == len(backends) {
			ready := true
			for _, backend := range backends {
				up := false
				for _, live := range status.Backends {
					if live.ServerID == fmt.Sprintf("node-%d", backend.ID) && live.Available != nil && *live.Available {
						up = true
					}
				}
				ready = ready && up
			}
			sampled, e := time.Parse(time.RFC3339, status.CheckedAt)
			if ready && e == nil && time.Since(sampled) >= 0 && time.Since(sampled) < time.Minute {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return errors.New("APPLY_UNVERIFIED")
		case <-time.After(time.Second):
		}
	}
}
