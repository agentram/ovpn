package cli

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"ovpn/internal/model"
	"ovpn/internal/util"
)

// canonicalGlobalUsers returns canonical users keyed by username.
// It enforces cross-server identity consistency for fields that must stay equal cluster-wide.
func (a *App) canonicalGlobalUsers() (map[string]model.User, error) {
	servers, err := a.listEnabledServers()
	if err != nil {
		return nil, err
	}
	return a.canonicalUsersFromServers(servers)
}

// canonicalUsersFromServers resolves canonical users from provided server list.
func (a *App) canonicalUsersFromServers(servers []model.Server) (map[string]model.User, error) {
	canonical := make(map[string]model.User)
	sourceByUsername := make(map[string]string)
	var conflicts []string

	for _, srv := range servers {
		users, err := a.store.ListUsers(a.ctx, srv.ID)
		if err != nil {
			return nil, err
		}
		for _, u := range users {
			key := strings.TrimSpace(u.Username)
			if key == "" {
				continue
			}
			prev, ok := canonical[key]
			if !ok {
				canonical[key] = u
				sourceByUsername[key] = srv.Name
				continue
			}
			for _, field := range diffGlobalIdentityFields(prev, u) {
				conflicts = append(conflicts, fmt.Sprintf(
					"user %q field %q differs: %s=%q, %s=%q",
					key,
					field,
					sourceByUsername[key],
					globalIdentityFieldValue(prev, field),
					srv.Name,
					globalIdentityFieldValue(u, field),
				))
			}
		}
	}

	if len(conflicts) > 0 {
		sort.Strings(conflicts)
		return nil, fmt.Errorf("global user consistency check failed:\n- %s", strings.Join(conflicts, "\n- "))
	}
	return canonical, nil
}

// ensureRealityParity verifies that cluster-critical REALITY parameters are equal on all registered servers.
func (a *App) ensureRealityParity() error {
	for _, role := range []string{model.ServerRoleVPN, model.ServerRoleProxy} {
		servers, err := a.listEnabledServersByRole(role)
		if err != nil {
			return err
		}
		if err := ensureRealityParityForServers(servers); err != nil {
			return err
		}
	}
	return nil
}

// ensureRealityParityForServers verifies REALITY parity for provided servers.
func ensureRealityParityForServers(servers []model.Server) error {
	if len(servers) <= 1 {
		return nil
	}
	base := servers[0]
	var issues []string
	for _, srv := range servers[1:] {
		diff := realityParityDiff(base, srv, false)
		if len(diff) == 0 {
			continue
		}
		issues = append(issues, fmt.Sprintf("server %s differs from %s in: %s", srv.Name, base.Name, strings.Join(diff, ", ")))
	}
	if len(issues) > 0 {
		sort.Strings(issues)
		return fmt.Errorf("REALITY parity check failed:\n- %s", strings.Join(issues, "\n- "))
	}
	return nil
}

func realityParityDiff(base model.Server, srv model.Server, includeProxyServiceUUID bool) []string {
	var diff []string
	if strings.TrimSpace(srv.RealityPrivateKey) != strings.TrimSpace(base.RealityPrivateKey) {
		diff = append(diff, "reality_private_key")
	}
	if strings.TrimSpace(srv.RealityPublicKey) != strings.TrimSpace(base.RealityPublicKey) {
		diff = append(diff, "reality_public_key")
	}
	if strings.TrimSpace(srv.RealityShortIDs) != strings.TrimSpace(base.RealityShortIDs) {
		diff = append(diff, "reality_short_ids")
	}
	if strings.TrimSpace(srv.RealityServerName) != strings.TrimSpace(base.RealityServerName) {
		diff = append(diff, "reality_server_name")
	}
	if strings.TrimSpace(srv.RealityTarget) != strings.TrimSpace(base.RealityTarget) {
		diff = append(diff, "reality_target")
	}
	if includeProxyServiceUUID && strings.TrimSpace(srv.ProxyServiceUUID) != strings.TrimSpace(base.ProxyServiceUUID) {
		diff = append(diff, "proxy_service_uuid")
	}
	return diff
}

// resolveUserMutationServers resolves target servers for mutating user operations.
// Users are always distributed across all enabled servers.
func (a *App) resolveUserMutationServers() ([]model.Server, error) {
	servers, err := a.listEnabledServers()
	if err != nil {
		return nil, err
	}
	if len(servers) == 0 {
		return nil, fmt.Errorf("no enabled servers found")
	}
	if len(servers) > 1 {
		if err := a.ensureRealityParity(); err != nil {
			return nil, err
		}
		if _, err := a.canonicalGlobalUsers(); err != nil {
			return nil, err
		}
	}
	return servers, nil
}

// listEnabledServers returns enabled servers in local state.
func (a *App) listEnabledServers() ([]model.Server, error) {
	servers, err := a.store.ListServers(a.ctx)
	if err != nil {
		return nil, err
	}
	out := make([]model.Server, 0, len(servers))
	for _, srv := range servers {
		if srv.Enabled {
			out = append(out, srv)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// listEnabledServersByRole returns enabled servers for the requested role.
func (a *App) listEnabledServersByRole(role string) ([]model.Server, error) {
	servers, err := a.listEnabledServers()
	if err != nil {
		return nil, err
	}
	if role == "" {
		return servers, nil
	}
	out := make([]model.Server, 0, len(servers))
	for _, srv := range servers {
		if srv.NormalizedRole() == role {
			out = append(out, srv)
		}
	}
	return out, nil
}

// materializeCanonicalUsersOnServer upserts canonical users into target server local state.
func (a *App) materializeCanonicalUsersOnServer(target model.Server) error {
	canonical, err := a.canonicalGlobalUsers()
	if err != nil {
		return err
	}
	if len(canonical) == 0 {
		return nil
	}
	users, err := a.store.ListUsers(a.ctx, target.ID)
	if err != nil {
		return err
	}
	current := make(map[string]*model.User, len(users))
	for i := range users {
		u := users[i]
		current[u.Username] = &u
	}

	usernames := make([]string, 0, len(canonical))
	for username := range canonical {
		usernames = append(usernames, username)
	}
	sort.Strings(usernames)

	for _, username := range usernames {
		want := canonical[username]
		if have, ok := current[username]; ok {
			if !applyCanonicalIdentity(have, want) {
				continue
			}
			if err := a.store.UpdateUser(a.ctx, have); err != nil {
				return err
			}
			continue
		}
		add := &model.User{
			ServerID:         target.ID,
			Username:         want.Username,
			UUID:             want.UUID,
			Email:            want.Email,
			Enabled:          want.Enabled,
			ExpiryDate:       cloneTimePtr(want.ExpiryDate),
			TrafficLimitByte: cloneInt64Ptr(want.TrafficLimitByte),
			QuotaEnabled:     want.QuotaEnabled,
			QuotaBlocked:     false,
			QuotaBlockedAt:   nil,
			Notes:            want.Notes,
			TagsCSV:          want.TagsCSV,
		}
		if err := a.store.AddUser(a.ctx, add); err != nil {
			return err
		}
	}
	return nil
}

// diffGlobalIdentityFields returns differing global-identity field names between users.
func diffGlobalIdentityFields(left model.User, right model.User) []string {
	var out []string
	if strings.TrimSpace(left.UUID) != strings.TrimSpace(right.UUID) {
		out = append(out, "uuid")
	}
	if strings.TrimSpace(left.Email) != strings.TrimSpace(right.Email) {
		out = append(out, "email")
	}
	if left.Enabled != right.Enabled {
		out = append(out, "enabled")
	}
	if normalizeTimePtr(left.ExpiryDate) != normalizeTimePtr(right.ExpiryDate) {
		out = append(out, "expiry_date")
	}
	if strings.TrimSpace(left.Notes) != strings.TrimSpace(right.Notes) {
		out = append(out, "notes")
	}
	if normalizeTagsCSV(left.TagsCSV) != normalizeTagsCSV(right.TagsCSV) {
		out = append(out, "tags")
	}
	return out
}

// globalIdentityFieldValue returns printable field value for user identity fields.
func globalIdentityFieldValue(u model.User, field string) string {
	switch field {
	case "uuid":
		return strings.TrimSpace(u.UUID)
	case "email":
		return strings.TrimSpace(u.Email)
	case "enabled":
		if u.Enabled {
			return "true"
		}
		return "false"
	case "expiry_date":
		return normalizeTimePtr(u.ExpiryDate)
	case "notes":
		return strings.TrimSpace(u.Notes)
	case "tags":
		return normalizeTagsCSV(u.TagsCSV)
	default:
		return ""
	}
}

// applyCanonicalIdentity applies global identity fields and reports whether user changed.
func applyCanonicalIdentity(dst *model.User, src model.User) bool {
	changed := false
	if strings.TrimSpace(dst.UUID) != strings.TrimSpace(src.UUID) {
		dst.UUID = strings.TrimSpace(src.UUID)
		changed = true
	}
	if strings.TrimSpace(dst.Email) != strings.TrimSpace(src.Email) {
		dst.Email = strings.TrimSpace(src.Email)
		changed = true
	}
	if dst.Enabled != src.Enabled {
		dst.Enabled = src.Enabled
		changed = true
	}
	if normalizeTimePtr(dst.ExpiryDate) != normalizeTimePtr(src.ExpiryDate) {
		dst.ExpiryDate = cloneTimePtr(src.ExpiryDate)
		changed = true
	}
	if strings.TrimSpace(dst.Notes) != strings.TrimSpace(src.Notes) {
		dst.Notes = strings.TrimSpace(src.Notes)
		changed = true
	}
	if normalizeTagsCSV(dst.TagsCSV) != normalizeTagsCSV(src.TagsCSV) {
		dst.TagsCSV = normalizeTagsCSV(src.TagsCSV)
		changed = true
	}
	return changed
}

// normalizeTimePtr returns RFC3339 UTC representation or empty value for nil.
func normalizeTimePtr(v *time.Time) string {
	if v == nil {
		return ""
	}
	return v.UTC().Format(time.RFC3339)
}

// normalizeTagsCSV returns normalized and sorted CSV tags.
func normalizeTagsCSV(raw string) string {
	tags := util.ParseCSV(raw)
	sort.Strings(tags)
	return util.JoinCSV(tags)
}

// cloneTimePtr clones time pointer.
func cloneTimePtr(v *time.Time) *time.Time {
	if v == nil {
		return nil
	}
	t := v.UTC()
	return &t
}

// cloneInt64Ptr clones int64 pointer.
func cloneInt64Ptr(v *int64) *int64 {
	if v == nil {
		return nil
	}
	c := *v
	return &c
}
