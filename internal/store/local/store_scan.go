package local

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"ovpn/internal/model"
)

// scanServer reads scan server from the local database.
func scanServer(row scanner) (*model.Server, error) {
	srv := &model.Server{}
	var created, updated string
	var lastDeploy sql.NullString
	var strict, enabled int
	if err := row.Scan(
		&srv.ID, &srv.Name, &srv.Role, &srv.Host, &srv.Domain, &srv.SSHUser, &srv.SSHPort, &srv.SSHIdentityFile, &srv.SSHKnownHostsFile,
		&strict, &srv.XrayVersion, &srv.RealityPrivateKey, &srv.RealityPublicKey, &srv.RealityShortIDs,
		&srv.RealityServerName, &srv.RealityTarget, &srv.ProxyServiceUUID, &enabled, &created, &updated, &lastDeploy,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("not found")
		}
		return nil, err
	}
	srv.SSHStrictHostKey = strict == 1
	srv.Enabled = enabled == 1
	srv.Role = model.NormalizeServerRole(srv.Role)
	srv.CreatedAt, _ = time.Parse(time.RFC3339, created)
	srv.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	if lastDeploy.Valid {
		t, _ := time.Parse(time.RFC3339, lastDeploy.String)
		srv.LastDeployAt = &t
	}
	realityPrivateKey, err := decryptSensitiveField(srv.RealityPrivateKey)
	if err != nil {
		return nil, err
	}
	srv.RealityPrivateKey = realityPrivateKey
	return srv, nil
}

// scanServerRows returns scan server rows.
func scanServerRows(rows *sql.Rows) (*model.Server, error) { return scanServer(rows) }

// scanUser reads scan user from the local database.
func scanUser(row scanner) (*model.User, error) {
	u := &model.User{}
	var created, updated string
	var enabled, quotaEnabled, quotaBlocked int
	var expiry sql.NullString
	var limit sql.NullInt64
	var quotaBlockedAt sql.NullString
	if err := row.Scan(&u.ID, &u.ServerID, &u.Username, &u.UUID, &u.Email, &enabled, &expiry, &limit, &quotaEnabled, &quotaBlocked, &quotaBlockedAt, &u.Notes, &u.TagsCSV, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("not found")
		}
		return nil, err
	}
	u.Enabled = enabled == 1
	u.QuotaEnabled = quotaEnabled == 1
	u.QuotaBlocked = quotaBlocked == 1
	u.CreatedAt, _ = time.Parse(time.RFC3339, created)
	u.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	if expiry.Valid {
		t, _ := time.Parse(time.RFC3339, expiry.String)
		u.ExpiryDate = &t
	}
	if limit.Valid {
		v := limit.Int64
		u.TrafficLimitByte = &v
	}
	if quotaBlockedAt.Valid {
		t, _ := time.Parse(time.RFC3339, quotaBlockedAt.String)
		u.QuotaBlockedAt = &t
	}
	return u, nil
}

// scanUserRows returns scan user rows.
func scanUserRows(rows *sql.Rows) (*model.User, error) { return scanUser(rows) }

type scanner interface {
	Scan(dest ...any) error
}

// boolToInt returns bool to int.
func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// nullableTime returns nullable time.
func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

// nullableInt64 returns nullable int 64.
func nullableInt64(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

// nullableInt64v returns nullable int 64 v.
func nullableInt64v(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}
