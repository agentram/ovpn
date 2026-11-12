package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"ovpn/internal/model"
)

func TestUnsupportedOnlineStatisticsAreUnknownInsteadOfOffline(t *testing.T) {
	t.Setenv("OVPN_USER_ONLINE_STATS", "")
	_, mux := newTestAgentMux(t)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/users/online", nil))
	var body model.UserPresence
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &body) != nil || body.Supported || body.SampledAt == "" {
		t.Fatal("old installation was reported as authoritative presence")
	}
	denied := httptest.NewRecorder()
	mux.ServeHTTP(denied, httptest.NewRequest(http.MethodPost, "/users/online", nil))
	if denied.Code != http.StatusMethodNotAllowed {
		t.Fatal("presence accepted a write")
	}
}
func TestUnavailableOnlineStatisticsDoNotReturnAnEmptySuccessfulSnapshot(t *testing.T) {
	t.Setenv("OVPN_USER_ONLINE_STATS", "true")
	_, mux := newTestAgentMux(t)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/users/online", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatal("unavailable Xray reported everyone offline")
	}
}
