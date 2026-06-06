package remote

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"ovpn/internal/model"
	"ovpn/internal/util"
)

const (
	DefaultConnectionRetention = 7 * 24 * time.Hour
	DefaultSourceBucketCap     = 256
	DefaultDebugEventCap       = 1000
	DefaultDebugSessionMax     = 24 * time.Hour
)

// RecordConnectionEvents persists parsed Xray access-log events as hourly aggregates.
func (s *Store) RecordConnectionEvents(ctx context.Context, events []model.ConnectionEvent, sourceBucketCap int, debugEventCap int, now time.Time) error {
	if len(events) == 0 {
		return nil
	}
	if sourceBucketCap <= 0 {
		sourceBucketCap = DefaultSourceBucketCap
	}
	if debugEventCap <= 0 {
		debugEventCap = DefaultDebugEventCap
	}
	now = now.UTC()
	active, err := s.activeDebugSessions(ctx, now)
	if err != nil {
		return err
	}
	debugCounts, err := s.debugEventCounts(ctx, active)
	if err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	nowText := util.NowUTC().Format(time.RFC3339)
	sourceCounts := make(map[string]int)
	for _, ev := range events {
		email := strings.TrimSpace(ev.Email)
		if email == "" {
			continue
		}
		ts := ev.Timestamp.UTC()
		if ts.IsZero() {
			ts = now
		}
		hour := ts.Truncate(time.Hour).Format(time.RFC3339)
		lastSeen := ts.Format(time.RFC3339)
		accepted, rejected := resultDeltas(ev.Result)
		ipv4, ipv6, domain, unknown := familyDeltas(ev.DestinationFamily)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO connection_hourly (
				email, window_start, accepted_count, rejected_count,
				dest_ipv4_count, dest_ipv6_count, dest_domain_count, dest_unknown_count,
				source_overflow_count, last_seen_at, updated_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)
			ON CONFLICT(email, window_start)
			DO UPDATE SET
				accepted_count=connection_hourly.accepted_count + excluded.accepted_count,
				rejected_count=connection_hourly.rejected_count + excluded.rejected_count,
				dest_ipv4_count=connection_hourly.dest_ipv4_count + excluded.dest_ipv4_count,
				dest_ipv6_count=connection_hourly.dest_ipv6_count + excluded.dest_ipv6_count,
				dest_domain_count=connection_hourly.dest_domain_count + excluded.dest_domain_count,
				dest_unknown_count=connection_hourly.dest_unknown_count + excluded.dest_unknown_count,
				last_seen_at=CASE
					WHEN connection_hourly.last_seen_at IS NULL OR excluded.last_seen_at > connection_hourly.last_seen_at THEN excluded.last_seen_at
					ELSE connection_hourly.last_seen_at
				END,
				updated_at=excluded.updated_at
		`, email, hour, accepted, rejected, ipv4, ipv6, domain, unknown, lastSeen, nowText); err != nil {
			return err
		}
		if ev.DestinationPort > 0 {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO connection_port_hourly (email, window_start, port, count, updated_at)
				VALUES (?, ?, ?, 1, ?)
				ON CONFLICT(email, window_start, port)
				DO UPDATE SET count=connection_port_hourly.count + 1, updated_at=excluded.updated_at
			`, email, hour, ev.DestinationPort, nowText); err != nil {
				return err
			}
		}
		sourceNetwork := strings.TrimSpace(ev.SourceNetwork)
		if sourceNetwork != "" {
			sourceKey := email + "\x00" + hour
			sourceBucket := sourceBucketHash(sourceNetwork)
			exists, err := sourceBucketExists(ctx, tx, email, hour, sourceBucket)
			if err != nil {
				return err
			}
			if !exists {
				current, ok := sourceCounts[sourceKey]
				if !ok {
					current, err = sourceBucketCount(ctx, tx, email, hour)
					if err != nil {
						return err
					}
				}
				if current >= sourceBucketCap {
					if _, err := tx.ExecContext(ctx, `
						UPDATE connection_hourly
						SET source_overflow_count=source_overflow_count + 1, updated_at=?
						WHERE email=? AND window_start=?
					`, nowText, email, hour); err != nil {
						return err
					}
				} else {
					if _, err := tx.ExecContext(ctx, `
						INSERT INTO connection_source_hourly (email, window_start, source_bucket, count, updated_at)
						VALUES (?, ?, ?, 1, ?)
					`, email, hour, sourceBucket, nowText); err != nil {
						return err
					}
					current++
				}
				sourceCounts[sourceKey] = current
			} else if _, err := tx.ExecContext(ctx, `
				UPDATE connection_source_hourly
				SET count=count + 1, updated_at=?
				WHERE email=? AND window_start=? AND source_bucket=?
			`, nowText, email, hour, sourceBucket); err != nil {
				return err
			}
		}
		if _, ok := active[email]; ok && debugCounts[email] < debugEventCap {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO connection_debug_events (
					email, ts, result, source_network, destination, destination_port, destination_family, created_at
				)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			`, email, ts.Format(time.RFC3339), normalizeConnectionResult(ev.Result), strings.TrimSpace(ev.SourceNetwork), strings.TrimSpace(ev.Destination), ev.DestinationPort, normalizeDestinationFamily(ev.DestinationFamily), nowText); err != nil {
				return err
			}
			debugCounts[email]++
		}
	}
	return tx.Commit()
}

func resultDeltas(result string) (accepted int, rejected int) {
	switch normalizeConnectionResult(result) {
	case "accepted":
		return 1, 0
	case "rejected":
		return 0, 1
	default:
		return 0, 0
	}
}

func familyDeltas(family string) (ipv4 int, ipv6 int, domain int, unknown int) {
	switch normalizeDestinationFamily(family) {
	case "ipv4":
		return 1, 0, 0, 0
	case "ipv6":
		return 0, 1, 0, 0
	case "domain":
		return 0, 0, 1, 0
	default:
		return 0, 0, 0, 1
	}
}

func normalizeConnectionResult(result string) string {
	switch strings.ToLower(strings.TrimSpace(result)) {
	case "accepted", "accept":
		return "accepted"
	case "rejected", "reject":
		return "rejected"
	default:
		return "unknown"
	}
}

func normalizeDestinationFamily(family string) string {
	switch strings.ToLower(strings.TrimSpace(family)) {
	case "ipv4", "ipv6", "domain":
		return strings.ToLower(strings.TrimSpace(family))
	default:
		return "unknown"
	}
}

func sourceBucketHash(sourceNetwork string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(sourceNetwork)))
	return hex.EncodeToString(sum[:])
}

func sourceBucketExists(ctx context.Context, tx *sql.Tx, email, hour, sourceBucket string) (bool, error) {
	var n int
	err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM connection_source_hourly
		WHERE email=? AND window_start=? AND source_bucket=?
	`, email, hour, sourceBucket).Scan(&n)
	return n > 0, err
}

func sourceBucketCount(ctx context.Context, tx *sql.Tx, email, hour string) (int, error) {
	var n int
	err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM connection_source_hourly
		WHERE email=? AND window_start=?
	`, email, hour).Scan(&n)
	return n, err
}

func (s *Store) activeDebugSessions(ctx context.Context, now time.Time) (map[string]model.ConnectionDebugSession, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT email, started_at, expires_at
		FROM connection_debug_sessions
		WHERE expires_at > ?
	`, now.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]model.ConnectionDebugSession)
	for rows.Next() {
		var row model.ConnectionDebugSession
		var started, expires string
		if err := rows.Scan(&row.Email, &started, &expires); err != nil {
			return nil, err
		}
		row.StartedAt, _ = time.Parse(time.RFC3339, started)
		row.ExpiresAt, _ = time.Parse(time.RFC3339, expires)
		out[row.Email] = row
	}
	return out, rows.Err()
}

func (s *Store) debugEventCounts(ctx context.Context, sessions map[string]model.ConnectionDebugSession) (map[string]int, error) {
	out := make(map[string]int, len(sessions))
	for email := range sessions {
		var n int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM connection_debug_events WHERE email=?`, email).Scan(&n); err != nil {
			return nil, err
		}
		out[email] = n
	}
	return out, nil
}

// ConnectionDiagnostics returns connection aggregates for one user in [since, until).
func (s *Store) ConnectionDiagnostics(ctx context.Context, email string, since time.Time, until time.Time, topPortsLimit int) (model.UserConnectionDiagnostics, error) {
	email = strings.TrimSpace(email)
	if topPortsLimit <= 0 {
		topPortsLimit = 5
	}
	if until.IsZero() {
		until = util.NowUTC()
	}
	if since.IsZero() || !since.Before(until) {
		since = until.Add(-24 * time.Hour)
	}
	since = since.UTC()
	until = until.UTC()
	out := model.UserConnectionDiagnostics{
		Email: email,
		Since: since.Format(time.RFC3339),
		Until: until.Format(time.RFC3339),
	}
	var lastSeen sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(accepted_count), 0),
			COALESCE(SUM(rejected_count), 0),
			COALESCE(SUM(dest_ipv4_count), 0),
			COALESCE(SUM(dest_ipv6_count), 0),
			COALESCE(SUM(dest_domain_count), 0),
			COALESCE(SUM(dest_unknown_count), 0),
			COALESCE(SUM(source_overflow_count), 0),
			MAX(last_seen_at)
		FROM connection_hourly
		WHERE email=? AND window_start >= ? AND window_start < ?
	`, email, since.Truncate(time.Hour).Format(time.RFC3339), until.Format(time.RFC3339)).Scan(
		&out.AcceptedCount,
		&out.RejectedCount,
		&out.DestinationIPv4Count,
		&out.DestinationIPv6Count,
		&out.DestinationDomainCount,
		&out.DestinationUnknownCount,
		&out.SourceNetworksOverflow,
		&lastSeen,
	)
	if err != nil {
		return model.UserConnectionDiagnostics{}, err
	}
	if lastSeen.Valid {
		if parsed, err := time.Parse(time.RFC3339, lastSeen.String); err == nil {
			t := parsed.UTC()
			out.LastSeenAt = &t
		}
	}
	var sources int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT source_bucket)
		FROM connection_source_hourly
		WHERE email=? AND window_start >= ? AND window_start < ?
	`, email, since.Truncate(time.Hour).Format(time.RFC3339), until.Format(time.RFC3339)).Scan(&sources); err != nil {
		return model.UserConnectionDiagnostics{}, err
	}
	out.ApproxSourceNetworks = sources
	ports, err := s.connectionTopPorts(ctx, email, since, until, topPortsLimit)
	if err != nil {
		return model.UserConnectionDiagnostics{}, err
	}
	out.TopPorts = ports
	if session, ok, err := s.GetDebugSession(ctx, email, until); err != nil {
		return model.UserConnectionDiagnostics{}, err
	} else if ok {
		out.DebugActive = true
		t := session.ExpiresAt.UTC()
		out.DebugExpiresAt = &t
	}
	return out, nil
}

func (s *Store) connectionTopPorts(ctx context.Context, email string, since time.Time, until time.Time, limit int) ([]model.ConnectionPortCount, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT port, COALESCE(SUM(count), 0) AS total
		FROM connection_port_hourly
		WHERE email=? AND window_start >= ? AND window_start < ?
		GROUP BY port
		ORDER BY total DESC, port ASC
		LIMIT ?
	`, email, since.UTC().Truncate(time.Hour).Format(time.RFC3339), until.UTC().Format(time.RFC3339), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ConnectionPortCount
	for rows.Next() {
		var row model.ConnectionPortCount
		if err := rows.Scan(&row.Port, &row.Count); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// ListConnectionMetricSnapshots returns 24h-style connection summaries grouped by user.
func (s *Store) ListConnectionMetricSnapshots(ctx context.Context, since time.Time, until time.Time) ([]model.UserConnectionDiagnostics, error) {
	since = since.UTC()
	until = until.UTC()
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			email,
			COALESCE(SUM(accepted_count), 0),
			COALESCE(SUM(rejected_count), 0),
			COALESCE(SUM(dest_ipv6_count), 0),
			MAX(last_seen_at)
		FROM connection_hourly
		WHERE window_start >= ? AND window_start < ?
		GROUP BY email
	`, since.Truncate(time.Hour).Format(time.RFC3339), until.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	var out []model.UserConnectionDiagnostics
	for rows.Next() {
		var row model.UserConnectionDiagnostics
		var lastSeen sql.NullString
		row.Since = since.Format(time.RFC3339)
		row.Until = until.Format(time.RFC3339)
		if err := rows.Scan(&row.Email, &row.AcceptedCount, &row.RejectedCount, &row.DestinationIPv6Count, &lastSeen); err != nil {
			return nil, err
		}
		if lastSeen.Valid {
			if parsed, err := time.Parse(time.RFC3339, lastSeen.String); err == nil {
				t := parsed.UTC()
				row.LastSeenAt = &t
			}
		}
		out = append(out, row)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		var sources int
		if err := s.db.QueryRowContext(ctx, `
			SELECT COUNT(DISTINCT source_bucket)
			FROM connection_source_hourly
			WHERE email=? AND window_start >= ? AND window_start < ?
		`, out[i].Email, since.Truncate(time.Hour).Format(time.RFC3339), until.Format(time.RFC3339)).Scan(&sources); err != nil {
			return nil, err
		}
		out[i].ApproxSourceNetworks = sources
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Email < out[j].Email })
	return out, nil
}

// UserTrafficBetween returns per-user traffic aggregated from hourly rows for [since, until).
func (s *Store) UserTrafficBetween(ctx context.Context, email string, since time.Time, until time.Time) (model.UserTraffic, error) {
	var out model.UserTraffic
	out.Email = strings.TrimSpace(email)
	out.WindowType = "custom"
	out.WindowStart = since.UTC()
	err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(uplink_bytes), 0), COALESCE(SUM(downlink_bytes), 0)
		FROM user_traffic_hourly
		WHERE email=? AND window_start >= ? AND window_start < ?
	`, out.Email, since.UTC().Truncate(time.Hour).Format(time.RFC3339), until.UTC().Format(time.RFC3339)).Scan(&out.UplinkBytes, &out.DownlinkBytes)
	return out, err
}

// StartDebugSession enables short-lived event capture for one user.
func (s *Store) StartDebugSession(ctx context.Context, email string, duration time.Duration, now time.Time) (model.ConnectionDebugSession, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return model.ConnectionDebugSession{}, fmt.Errorf("email is required")
	}
	if duration <= 0 {
		duration = 15 * time.Minute
	}
	if duration > DefaultDebugSessionMax {
		duration = DefaultDebugSessionMax
	}
	now = now.UTC()
	session := model.ConnectionDebugSession{
		Email:     email,
		StartedAt: now,
		ExpiresAt: now.Add(duration),
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO connection_debug_sessions (email, started_at, expires_at, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(email)
		DO UPDATE SET started_at=excluded.started_at, expires_at=excluded.expires_at, updated_at=excluded.updated_at
	`, session.Email, session.StartedAt.Format(time.RFC3339), session.ExpiresAt.Format(time.RFC3339), now.Format(time.RFC3339))
	return session, err
}

// StopDebugSession disables event capture for one user.
func (s *Store) StopDebugSession(ctx context.Context, email string) error {
	email = strings.TrimSpace(email)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM connection_debug_sessions WHERE email=?`, email); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM connection_debug_events WHERE email=?`, email); err != nil {
		return err
	}
	return tx.Commit()
}

// GetDebugSession returns an active debug session for email at now.
func (s *Store) GetDebugSession(ctx context.Context, email string, now time.Time) (model.ConnectionDebugSession, bool, error) {
	var row model.ConnectionDebugSession
	var started, expires string
	err := s.db.QueryRowContext(ctx, `
		SELECT email, started_at, expires_at
		FROM connection_debug_sessions
		WHERE email=? AND expires_at > ?
	`, strings.TrimSpace(email), now.UTC().Format(time.RFC3339)).Scan(&row.Email, &started, &expires)
	if err == sql.ErrNoRows {
		return model.ConnectionDebugSession{}, false, nil
	}
	if err != nil {
		return model.ConnectionDebugSession{}, false, err
	}
	row.StartedAt, _ = time.Parse(time.RFC3339, started)
	row.ExpiresAt, _ = time.Parse(time.RFC3339, expires)
	return row, true, nil
}

// ListDebugSessions returns active debug sessions at now.
func (s *Store) ListDebugSessions(ctx context.Context, now time.Time) ([]model.ConnectionDebugSession, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT email, started_at, expires_at
		FROM connection_debug_sessions
		WHERE expires_at > ?
		ORDER BY email ASC
	`, now.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ConnectionDebugSession
	for rows.Next() {
		var row model.ConnectionDebugSession
		var started, expires string
		if err := rows.Scan(&row.Email, &started, &expires); err != nil {
			return nil, err
		}
		row.StartedAt, _ = time.Parse(time.RFC3339, started)
		row.ExpiresAt, _ = time.Parse(time.RFC3339, expires)
		out = append(out, row)
	}
	return out, rows.Err()
}

// ListDebugEvents returns captured debug events for one user.
func (s *Store) ListDebugEvents(ctx context.Context, email string, since time.Time, until time.Time, limit int) ([]model.ConnectionDebugEvent, error) {
	if limit <= 0 || limit > DefaultDebugEventCap {
		limit = DefaultDebugEventCap
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT ts, result, email, source_network, destination, destination_port, destination_family
		FROM connection_debug_events
		WHERE email=? AND ts >= ? AND ts < ?
		ORDER BY ts ASC, id ASC
		LIMIT ?
	`, strings.TrimSpace(email), since.UTC().Format(time.RFC3339), until.UTC().Format(time.RFC3339), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ConnectionDebugEvent
	for rows.Next() {
		var row model.ConnectionDebugEvent
		var ts string
		if err := rows.Scan(&ts, &row.Result, &row.Email, &row.SourceNetwork, &row.Destination, &row.DestinationPort, &row.DestinationFamily); err != nil {
			return nil, err
		}
		row.Timestamp, _ = time.Parse(time.RFC3339, ts)
		out = append(out, row)
	}
	return out, rows.Err()
}

// CleanupConnectionDiagnostics prunes expired sessions, old aggregates, and debug events.
func (s *Store) CleanupConnectionDiagnostics(ctx context.Context, now time.Time, retention time.Duration) error {
	if retention <= 0 {
		retention = DefaultConnectionRetention
	}
	cutoff := now.UTC().Add(-retention).Format(time.RFC3339)
	nowText := now.UTC().Format(time.RFC3339)
	for _, stmt := range []struct {
		query string
		arg   string
	}{
		{
			query: `DELETE FROM connection_debug_events
				WHERE email IN (SELECT email FROM connection_debug_sessions WHERE expires_at <= ?)`,
			arg: nowText,
		},
		{query: `DELETE FROM connection_debug_sessions WHERE expires_at <= ?`, arg: nowText},
		{query: `DELETE FROM connection_debug_events WHERE ts < ?`, arg: cutoff},
		{query: `DELETE FROM connection_hourly WHERE window_start < ?`, arg: cutoff},
		{query: `DELETE FROM connection_source_hourly WHERE window_start < ?`, arg: cutoff},
		{query: `DELETE FROM connection_port_hourly WHERE window_start < ?`, arg: cutoff},
	} {
		if _, err := s.db.ExecContext(ctx, stmt.query, stmt.arg); err != nil {
			return err
		}
	}
	return nil
}
