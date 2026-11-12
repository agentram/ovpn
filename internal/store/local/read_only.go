package local

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"

	"ovpn/internal/model"
)

// OpenReadOnly opens existing state without creation or schema migration.
func OpenReadOnly(ctx context.Context, dataDir string) (*Store, error) {
	path, err := filepath.Abs(filepath.Join(dataDir, "ovpn.db"))
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	u, err := url.Parse(sqliteDSN(path))
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("mode", "ro")
	q.Add("_pragma", "query_only(1)")
	u.RawQuery = q.Encode()
	db, err := sql.Open(sqliteDriver, u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// InventorySummary does not select credentials or user identities.
func (s *Store) InventorySummary(ctx context.Context) (servers, enabled, users int, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(enabled=1),0),
		(SELECT COUNT(DISTINCT username) FROM users) FROM servers`).Scan(&servers, &enabled, &users)
	return
}

// ListServerInventory projects only non-credential columns and supports keyset paging.
func (s *Store) ListServerInventory(ctx context.Context, after int64, limit int) ([]model.Server, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,role,enabled,primary_profile,enabled_profiles,xray_version,proxy_preset
		FROM servers WHERE id>? ORDER BY id LIMIT ?`, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.Server, 0)
	for rows.Next() {
		var srv model.Server
		var enabled int
		if err := rows.Scan(&srv.ID, &srv.Name, &srv.Role, &enabled, &srv.PrimaryProfile, &srv.EnabledProfiles, &srv.XrayVersion, &srv.ProxyPreset); err != nil {
			return nil, err
		}
		srv.Enabled = enabled == 1
		items = append(items, srv)
	}
	return items, rows.Err()
}

// GetClientServer selects client-side settings without reading server private keys or SSH material.
func (s *Store) GetClientServer(ctx context.Context, id int64) (*model.Server, error) {
	var srv model.Server
	var enabled int
	err := s.db.QueryRowContext(ctx, `SELECT id,name,role,host,domain,enabled,reality_public_key,reality_short_ids,
		reality_server_name,primary_profile,enabled_profiles,vless_client_encryption FROM servers WHERE id=?`, id).Scan(
		&srv.ID, &srv.Name, &srv.Role, &srv.Host, &srv.Domain, &enabled, &srv.RealityPublicKey, &srv.RealityShortIDs,
		&srv.RealityServerName, &srv.PrimaryProfile, &srv.EnabledProfiles, &srv.VLESSClientEncryption)
	if err != nil {
		return nil, err
	}
	srv.Enabled = enabled == 1
	srv.VLESSClientEncryption, err = decryptSensitiveField(srv.VLESSClientEncryption)
	if err != nil {
		return nil, err
	}
	return &srv, nil
}
