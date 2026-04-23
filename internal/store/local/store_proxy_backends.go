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

// AddProxyBackend writes proxy/backend mapping changes to the local database.
func (s *Store) AddProxyBackend(ctx context.Context, pb *model.ProxyBackend) error {
	now := util.NowUTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO proxy_backends (
			proxy_server_id, backend_server_id, enabled, priority, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?)
	`, pb.ProxyServerID, pb.BackendServerID, boolToInt(pb.Enabled), pb.Priority, now, now)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	pb.ID = id
	return nil
}

// UpsertProxyBackend writes proxy/backend mapping changes to the local database.
func (s *Store) UpsertProxyBackend(ctx context.Context, pb *model.ProxyBackend) error {
	now := util.NowUTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO proxy_backends (
			proxy_server_id, backend_server_id, enabled, priority, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(proxy_server_id, backend_server_id) DO UPDATE SET
			enabled=excluded.enabled,
			priority=excluded.priority,
			updated_at=excluded.updated_at
	`, pb.ProxyServerID, pb.BackendServerID, boolToInt(pb.Enabled), pb.Priority, now, now)
	return err
}

// DeleteProxyBackend writes proxy/backend mapping delete changes to the local database.
func (s *Store) DeleteProxyBackend(ctx context.Context, proxyServerID, backendServerID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM proxy_backends WHERE proxy_server_id=? AND backend_server_id=?`, proxyServerID, backendServerID)
	return err
}

// ListProxyBackends reads proxy/backend mappings from the local database.
func (s *Store) ListProxyBackends(ctx context.Context, proxyServerID int64) ([]model.ProxyBackend, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id, proxy_server_id, backend_server_id, enabled, priority, created_at, updated_at
		FROM proxy_backends pb
		WHERE pb.proxy_server_id=?
		ORDER BY pb.priority ASC, pb.backend_server_id ASC
	`, proxyServerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ProxyBackend
	for rows.Next() {
		var pb model.ProxyBackend
		var enabled int
		var created, updated string
		if err := rows.Scan(
			&pb.ID, &pb.ProxyServerID, &pb.BackendServerID, &enabled, &pb.Priority, &created, &updated,
		); err != nil {
			return nil, err
		}
		pb.Enabled = enabled == 1
		pb.CreatedAt, _ = time.Parse(time.RFC3339, created)
		pb.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
		out = append(out, pb)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range out {
		full, err := s.GetServerByID(ctx, out[i].BackendServerID)
		if err != nil {
			return nil, fmt.Errorf("load backend server %d: %w", out[i].BackendServerID, err)
		}
		out[i].BackendServer = full
	}
	return out, nil
}

// ListAttachedBackendServers reads attached backend servers from the local database.
func (s *Store) ListAttachedBackendServers(ctx context.Context, proxyServerID int64) ([]model.Server, error) {
	mappings, err := s.ListProxyBackends(ctx, proxyServerID)
	if err != nil {
		return nil, err
	}
	out := make([]model.Server, 0, len(mappings))
	for _, pb := range mappings {
		if !pb.Enabled || pb.BackendServer == nil {
			continue
		}
		out = append(out, *pb.BackendServer)
	}
	return out, nil
}

// BackendHasAttachedProxy reports whether the backend participates in at least one enabled proxy mapping.
func (s *Store) BackendHasAttachedProxy(ctx context.Context, backendServerID int64) (bool, error) {
	var found int
	err := s.db.QueryRowContext(ctx, `
		SELECT 1
		FROM proxy_backends
		WHERE backend_server_id=? AND enabled=1
		LIMIT 1
	`, backendServerID).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return found == 1, nil
}
