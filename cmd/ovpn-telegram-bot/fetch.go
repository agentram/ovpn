package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"ovpn/internal/model"
)

// fetchHealth returns health for callers.
func (b *bot) fetchHealth(ctx context.Context) (agentHealth, error) {
	var health agentHealth
	if err := b.fetchJSON(ctx, strings.TrimRight(b.cfg.agentURL, "/")+"/health", &health); err != nil {
		return agentHealth{}, err
	}
	return health, nil
}

// fetchSelfHealth returns bot self-health for callers.
func (b *bot) fetchSelfHealth(ctx context.Context) (selfHealthResponse, error) {
	var out selfHealthResponse
	if err := b.fetchJSON(ctx, strings.TrimSpace(b.cfg.selfURL), &out); err != nil {
		return selfHealthResponse{}, err
	}
	return out, nil
}

// fetchActiveAlerts returns active alert count from Alertmanager API.
func (b *bot) fetchActiveAlerts(ctx context.Context) (int, error) {
	base, err := url.Parse(strings.TrimSpace(b.cfg.alertmanagerURL))
	if err != nil {
		return 0, err
	}
	base.Path = "/api/v2/alerts"
	q := base.Query()
	q.Set("active", "true")
	q.Set("silenced", "false")
	q.Set("inhibited", "false")
	q.Set("unprocessed", "true")
	base.RawQuery = q.Encode()
	var rows []map[string]any
	if err := b.fetchJSON(ctx, base.String(), &rows); err != nil {
		return 0, err
	}
	return len(rows), nil
}

// fetchTotals returns totals for callers.
func (b *bot) fetchTotals(ctx context.Context) ([]model.UserTraffic, error) {
	var rows []model.UserTraffic
	if err := b.fetchJSON(ctx, strings.TrimRight(b.cfg.agentURL, "/")+"/stats/total", &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// fetchDaily returns daily for callers.
func (b *bot) fetchDaily(ctx context.Context, day time.Time) ([]model.UserTraffic, error) {
	var rows []model.UserTraffic
	url := strings.TrimRight(b.cfg.agentURL, "/") + "/stats/daily?date=" + day.UTC().Format("2006-01-02")
	if err := b.fetchJSON(ctx, url, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// fetchQuotaStatus returns quota status for callers.
func (b *bot) fetchQuotaStatus(ctx context.Context) (model.QuotaStatusResponse, error) {
	var out model.QuotaStatusResponse
	if err := b.fetchJSON(ctx, strings.TrimRight(b.cfg.agentURL, "/")+"/quota/status", &out); err != nil {
		return model.QuotaStatusResponse{}, err
	}
	return out, nil
}

// fetchUserStatus returns mirrored user status for callers.
func (b *bot) fetchUserStatus(ctx context.Context) (model.UserStatusResponse, error) {
	var out model.UserStatusResponse
	if err := b.fetchJSON(ctx, strings.TrimRight(b.cfg.agentURL, "/")+"/users/status", &out); err != nil {
		return model.UserStatusResponse{}, err
	}
	return out, nil
}

// fetchQuotaPolicies returns quota policies for callers.
func (b *bot) fetchQuotaPolicies(ctx context.Context) ([]model.QuotaUserPolicy, error) {
	var out []model.QuotaUserPolicy
	if err := b.fetchJSON(ctx, strings.TrimRight(b.cfg.agentURL, "/")+"/quota/policies", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// fetchJSON fetches json from configured HTTP endpoints.
func (b *bot) fetchJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("request failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// probe handles probe HTTP behavior for this service.
func (b *bot) probe(ctx context.Context, name, url string) monitorStatus {
	pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(pctx, http.MethodGet, url, nil)
	if err != nil {
		return monitorStatus{Name: name, Healthy: false, Detail: err.Error()}
	}
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return monitorStatus{Name: name, Healthy: false, Detail: err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return monitorStatus{Name: name, Healthy: true}
	}
	return monitorStatus{Name: name, Healthy: false, Detail: fmt.Sprintf("status %d", resp.StatusCode)}
}
