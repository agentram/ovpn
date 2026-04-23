package local

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"ovpn/internal/model"
	"ovpn/internal/util"
)

func normalizeServerForStorage(srv *model.Server) {
	srv.Role = model.NormalizeServerRole(srv.Role)
	if srv.Role == "" {
		srv.Role = model.ServerRoleVPN
	}
	if srv.Role == model.ServerRoleProxy {
		srv.ProxyPreset = model.NormalizeProxyPreset(srv.ProxyPreset)
	}
}

// AddServer writes server changes to the local database.
func (s *Store) AddServer(ctx context.Context, srv *model.Server) error {
	normalizeServerForStorage(srv)
	if err := srv.Validate(); err != nil {
		return err
	}
	realityPrivateKey, err := encryptSensitiveField(srv.RealityPrivateKey)
	if err != nil {
		return err
	}
	now := util.NowUTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO servers (
			name, role, host, domain, ssh_user, ssh_port, ssh_identity_file, ssh_known_hosts_file,
			ssh_strict_host_key, xray_version, reality_private_key, reality_public_key,
			reality_short_ids, reality_server_name, reality_target, proxy_preset, proxy_service_uuid, enabled, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, srv.Name, srv.Role, srv.Host, srv.Domain, srv.SSHUser, srv.SSHPort, srv.SSHIdentityFile, srv.SSHKnownHostsFile,
		boolToInt(srv.SSHStrictHostKey), srv.XrayVersion, realityPrivateKey, srv.RealityPublicKey,
		srv.RealityShortIDs, srv.RealityServerName, srv.RealityTarget, srv.ProxyPreset, srv.ProxyServiceUUID, boolToInt(srv.Enabled), now, now)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	srv.ID = id
	return nil
}

// UpdateServer writes server changes to the local database.
func (s *Store) UpdateServer(ctx context.Context, srv *model.Server) error {
	normalizeServerForStorage(srv)
	if err := srv.Validate(); err != nil {
		return err
	}
	realityPrivateKey, err := encryptSensitiveField(srv.RealityPrivateKey)
	if err != nil {
		return err
	}
	now := util.NowUTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx, `
		UPDATE servers SET
			role=?, host=?, domain=?, ssh_user=?, ssh_port=?, ssh_identity_file=?, ssh_known_hosts_file=?,
			ssh_strict_host_key=?, xray_version=?, reality_private_key=?, reality_public_key=?,
			reality_short_ids=?, reality_server_name=?, reality_target=?, proxy_preset=?, proxy_service_uuid=?, enabled=?, updated_at=?
		WHERE id=?
	`, srv.Role, srv.Host, srv.Domain, srv.SSHUser, srv.SSHPort, srv.SSHIdentityFile, srv.SSHKnownHostsFile,
		boolToInt(srv.SSHStrictHostKey), srv.XrayVersion, realityPrivateKey, srv.RealityPublicKey,
		srv.RealityShortIDs, srv.RealityServerName, srv.RealityTarget, srv.ProxyPreset, srv.ProxyServiceUUID, boolToInt(srv.Enabled), now, srv.ID)
	return err
}

// SetServerLastDeploy writes server last deploy changes to the local database.
func (s *Store) SetServerLastDeploy(ctx context.Context, serverID int64) error {
	now := util.NowUTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `UPDATE servers SET last_deploy_at=?, updated_at=? WHERE id=?`, now, now, serverID)
	return err
}

// GetServerByName reads server by name from the local database.
func (s *Store) GetServerByName(ctx context.Context, name string) (*model.Server, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, role, host, domain, ssh_user, ssh_port, ssh_identity_file, ssh_known_hosts_file,
			ssh_strict_host_key, xray_version, reality_private_key, reality_public_key, reality_short_ids,
			reality_server_name, reality_target, proxy_preset, proxy_service_uuid, enabled, created_at, updated_at, last_deploy_at
		FROM servers WHERE name=?
	`, name)
	return scanServer(row)
}

// GetServerByID reads server by id from the local database.
func (s *Store) GetServerByID(ctx context.Context, id int64) (*model.Server, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, role, host, domain, ssh_user, ssh_port, ssh_identity_file, ssh_known_hosts_file,
			ssh_strict_host_key, xray_version, reality_private_key, reality_public_key, reality_short_ids,
			reality_server_name, reality_target, proxy_preset, proxy_service_uuid, enabled, created_at, updated_at, last_deploy_at
		FROM servers WHERE id=?
	`, id)
	return scanServer(row)
}

// ListServers reads servers from the local database.
func (s *Store) ListServers(ctx context.Context) ([]model.Server, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, role, host, domain, ssh_user, ssh_port, ssh_identity_file, ssh_known_hosts_file,
			ssh_strict_host_key, xray_version, reality_private_key, reality_public_key, reality_short_ids,
			reality_server_name, reality_target, proxy_preset, proxy_service_uuid, enabled, created_at, updated_at, last_deploy_at
		FROM servers ORDER BY id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Server
	for rows.Next() {
		srv, err := scanServerRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *srv)
	}
	return out, rows.Err()
}

// DeleteServerByName writes server by name changes to the local database.
func (s *Store) DeleteServerByName(ctx context.Context, name string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var serverID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM servers WHERE name=?`, name).Scan(&serverID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("not found")
		}
		return err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM backup_records WHERE server_id=?`, serverID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM servers WHERE id=?`, serverID); err != nil {
		return err
	}
	return tx.Commit()
}
