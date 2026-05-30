package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireAgentToken(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	cases := []struct {
		name       string
		token      string
		method     string
		path       string
		authHeader string
		wantStatus int
	}{
		{name: "disabled allows mutating without token", token: "", method: http.MethodPost, path: "/runtime/user/add", wantStatus: http.StatusOK},
		{name: "read-only path stays open", token: "secret", method: http.MethodGet, path: "/stats/total", wantStatus: http.StatusOK},
		{name: "metrics stays open for prometheus", token: "secret", method: http.MethodGet, path: "/metrics", wantStatus: http.StatusOK},
		{name: "mutating without token rejected", token: "secret", method: http.MethodPost, path: "/runtime/user/add", wantStatus: http.StatusUnauthorized},
		{name: "mutating with wrong token rejected", token: "secret", method: http.MethodPost, path: "/runtime/user/add", authHeader: "Bearer nope", wantStatus: http.StatusUnauthorized},
		{name: "mutating with correct token allowed", token: "secret", method: http.MethodPost, path: "/runtime/user/add", authHeader: "Bearer secret", wantStatus: http.StatusOK},
		{name: "missing bearer prefix rejected", token: "secret", method: http.MethodPost, path: "/runtime/user/add", authHeader: "secret", wantStatus: http.StatusUnauthorized},
		// A side-effecting GET must not bypass auth just because it is not POST.
		{name: "side-effecting GET still requires token", token: "secret", method: http.MethodGet, path: "/collect", wantStatus: http.StatusUnauthorized},
		{name: "side-effecting GET allowed with token", token: "secret", method: http.MethodGet, path: "/collect", authHeader: "Bearer secret", wantStatus: http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := requireAgentToken(tc.token, next)
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
		})
	}
}
