package local

import (
	"context"
	"encoding/json"

	"ovpn/internal/model"
)

// SetSubscriptionNode opts a server into publication. Its applied configuration
// is saved separately after deployment has been verified.
func (s *Store) SetSubscriptionNode(ctx context.Context, id int64, publish bool) error {
	if !publish {
		_, err := s.db.ExecContext(ctx, "DELETE FROM subscription_nodes WHERE server_id = ?", id)
		return err
	}
	_, err := s.db.ExecContext(ctx, "INSERT INTO subscription_nodes (server_id) VALUES (?) ON CONFLICT DO NOTHING", id)
	return err
}

func (s *Store) ClearAppliedSubscriptionNode(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, "UPDATE subscription_nodes SET applied_config = NULL WHERE server_id = ?", id)
	return err
}

func (s *Store) SaveAppliedSubscriptionNode(ctx context.Context, server model.Server) error {
	// Only public client configuration is needed for link generation.
	server.RealityPrivateKey = ""
	server.ProxyServiceUUID = ""
	server.SSHIdentityFile = ""
	server.SSHKnownHostsFile = ""
	server.VLESSServerDecryption = ""
	raw, err := json.Marshal(server)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, "UPDATE subscription_nodes SET applied_config = ? WHERE server_id = ?", raw, server.ID)
	return err
}

func (s *Store) SubscriptionNodes(ctx context.Context) ([]model.Server, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT n.applied_config FROM subscription_nodes n
		JOIN servers s ON s.id = n.server_id WHERE s.enabled = 1 AND n.applied_config IS NOT NULL ORDER BY s.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var servers []model.Server
	for rows.Next() {
		var raw []byte
		var server model.Server
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &server); err != nil {
			return nil, err
		}
		servers = append(servers, server)
	}
	return servers, rows.Err()
}
