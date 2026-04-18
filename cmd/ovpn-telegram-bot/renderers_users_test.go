package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestRenderUsersListCompactSortsBlockedFirst(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/users/status") {
			body := `{
				"effective_enabled_users": 1,
				"expiring_2d_users": 1,
				"expired_users": 0,
				"users": [
					{"username":"alpha","email":"alpha@test","quota_enabled":true,"blocked_by_quota":false,"window_30d_usage_byte":90,"window_30d_quota_byte":100,"effective_enabled":true,"expiry_date":"2026-04-18","days_until_expiry":1.0},
					{"username":"blocked","email":"blocked@test","quota_enabled":true,"blocked_by_quota":true,"window_30d_usage_byte":10,"window_30d_quota_byte":100,"effective_enabled":true}
				]
			}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		}
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(`{"error":"not found"}`)), Header: make(http.Header)}, nil
	})}

	b := &bot{httpClient: client, cfg: config{agentURL: "http://ovpn-agent:9090"}}
	out, err := b.renderUsersList(context.Background())
	if err != nil {
		t.Fatalf("renderUsersList: %v", err)
	}
	joined := strings.Join(out, "\n")
	if !strings.Contains(joined, "<pre>") {
		t.Fatalf("expected HTML pre block, got %q", joined)
	}
	if !strings.Contains(joined, "Users Audit") {
		t.Fatalf("expected users header, got %q", joined)
	}
	if strings.Index(joined, "blocked") > strings.Index(joined, "alpha") {
		t.Fatalf("expected blocked user first, got %q", joined)
	}
	if strings.Contains(joined, "blocked@test") || strings.Contains(joined, "alpha@test") {
		t.Fatalf("expected username-only labels, got %q", joined)
	}
	if !strings.Contains(joined, "blocked") {
		t.Fatalf("expected blocked status marker, got %q", joined)
	}
	if !strings.Contains(joined, "2026-04-18") {
		t.Fatalf("expected expiry date in output, got %q", joined)
	}
}
