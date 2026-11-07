package local

import (
	"context"
	"database/sql"
	"time"
)

func (s *Store) ListCheckUsernames(ctx context.Context, serverID int64, after string, limit int, now time.Time) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT u.username FROM users u JOIN servers s ON s.id=u.server_id
		WHERE s.id=? AND s.enabled=1 AND u.enabled=1 AND u.quota_blocked=0 AND u.username>?
		AND (u.expiry_date IS NULL OR julianday(u.expiry_date)>julianday(?))
		ORDER BY u.username LIMIT ?`, serverID, after, now.UTC().Format(time.RFC3339Nano), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

type UserInventory struct {
	Username                                                                                string
	ConfiguredNodes, EnabledNodes, EligibleNodes, DisabledNodes, ExpiredNodes, BlockedNodes int
	EarliestExpiry                                                                          string
	QuotaMinBytes, QuotaMaxBytes                                                            int64
	DefaultQuotaNodes, UnmeteredNodes                                                       int
	PolicyDiff, IdentityConflict                                                            bool
}

// ListUserInventory groups legacy per-server records without returning credentials or contact details.
func (s *Store) ListUserInventory(ctx context.Context, after string, limit int, now time.Time) ([]UserInventory, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT u.username,COUNT(*),SUM(s.enabled=1),
		SUM(CASE WHEN s.enabled=1 AND u.enabled=1 AND u.quota_blocked=0 AND (u.expiry_date IS NULL OR julianday(u.expiry_date)>julianday(?)) THEN 1 ELSE 0 END),
		SUM(s.enabled=1 AND u.enabled=0),
		SUM(CASE WHEN s.enabled=1 AND u.expiry_date IS NOT NULL AND julianday(u.expiry_date)<=julianday(?) THEN 1 ELSE 0 END),
		SUM(s.enabled=1 AND u.quota_blocked=1),
		MIN(CASE WHEN s.enabled=1 THEN u.expiry_date END),
		COALESCE(MIN(CASE WHEN s.enabled=1 AND u.quota_enabled=1 AND u.traffic_limit_byte>0 THEN u.traffic_limit_byte END),0),
		COALESCE(MAX(CASE WHEN s.enabled=1 AND u.quota_enabled=1 AND u.traffic_limit_byte>0 THEN u.traffic_limit_byte END),0),
		SUM(s.enabled=1 AND u.quota_enabled=1 AND COALESCE(u.traffic_limit_byte,0)<=0),
		SUM(s.enabled=1 AND u.quota_enabled=0),
		COUNT(DISTINCT CASE WHEN s.enabled=1 THEN u.enabled END)>1 OR
		COUNT(DISTINCT CASE WHEN s.enabled=1 THEN COALESCE(u.expiry_date,'') END)>1 OR
 COUNT(DISTINCT CASE WHEN s.enabled=1 THEN u.quota_enabled END)>1 OR
 COUNT(DISTINCT CASE WHEN s.enabled=1 THEN u.notes END)>1 OR
 COUNT(DISTINCT CASE WHEN s.enabled=1 THEN u.tags_csv END)>1,
		COUNT(DISTINCT CASE WHEN s.enabled=1 THEN u.uuid END)>1 OR COUNT(DISTINCT CASE WHEN s.enabled=1 THEN u.email END)>1
		FROM users u JOIN servers s ON s.id=u.server_id
		WHERE u.username>? GROUP BY u.username ORDER BY u.username LIMIT ?`, now.UTC().Format(time.RFC3339), now.UTC().Format(time.RFC3339), after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]UserInventory, 0)
	for rows.Next() {
		var user UserInventory
		var expiry sql.NullString
		if err := rows.Scan(&user.Username, &user.ConfiguredNodes, &user.EnabledNodes, &user.EligibleNodes, &user.DisabledNodes,
			&user.ExpiredNodes, &user.BlockedNodes, &expiry, &user.QuotaMinBytes, &user.QuotaMaxBytes, &user.DefaultQuotaNodes, &user.UnmeteredNodes, &user.PolicyDiff, &user.IdentityConflict); err != nil {
			return nil, err
		}
		user.EarliestExpiry = expiry.String
		user.PolicyDiff = user.PolicyDiff || user.QuotaMinBytes != user.QuotaMaxBytes
		user.PolicyDiff = user.PolicyDiff || user.DefaultQuotaNodes > 0 && user.DefaultQuotaNodes < user.EnabledNodes || user.UnmeteredNodes > 0 && user.UnmeteredNodes < user.EnabledNodes
		items = append(items, user)
	}
	return items, rows.Err()
}
