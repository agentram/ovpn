package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHAEndpointKeepsUnsupportedUnknownAndRequiresTheAgentToken(t *testing.T) {
	_, mux := newTestAgentMux(t)
	secured := requireAgentToken("fixture-agent-token", mux)
	request := httptest.NewRequest(http.MethodGet, "/ha/status", nil)
	denied := httptest.NewRecorder()
	secured.ServeHTTP(denied, request)
	if denied.Code != http.StatusUnauthorized {
		t.Fatal("HA metrics bypassed agent auth")
	}
	request.Header.Set("Authorization", "Bearer fixture-agent-token")
	result := httptest.NewRecorder()
	secured.ServeHTTP(result, request)
	if result.Code != http.StatusOK || !strings.Contains(result.Body.String(), `"supported":false`) || !strings.Contains(result.Body.String(), `"state":"unknown"`) {
		t.Fatal("unsupported HA invented a successful pool")
	}
	post := httptest.NewRequest(http.MethodPost, "/ha/status", nil)
	post.Header.Set("Authorization", "Bearer fixture-agent-token")
	result = httptest.NewRecorder()
	secured.ServeHTTP(result, post)
	if result.Code != http.StatusMethodNotAllowed {
		t.Fatal("write accepted on read endpoint")
	}
}
