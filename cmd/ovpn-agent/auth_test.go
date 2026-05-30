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
		authHeader string
		wantStatus int
	}{
		{name: "disabled allows mutating without token", token: "", method: http.MethodPost, wantStatus: http.StatusOK},
		{name: "read-only stays open", token: "secret", method: http.MethodGet, wantStatus: http.StatusOK},
		{name: "mutating without token rejected", token: "secret", method: http.MethodPost, wantStatus: http.StatusUnauthorized},
		{name: "mutating with wrong token rejected", token: "secret", method: http.MethodPost, authHeader: "Bearer nope", wantStatus: http.StatusUnauthorized},
		{name: "mutating with correct token allowed", token: "secret", method: http.MethodPost, authHeader: "Bearer secret", wantStatus: http.StatusOK},
		{name: "missing bearer prefix rejected", token: "secret", method: http.MethodPost, authHeader: "secret", wantStatus: http.StatusUnauthorized},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := requireAgentToken(tc.token, next)
			req := httptest.NewRequest(tc.method, "/runtime/user/add", nil)
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
