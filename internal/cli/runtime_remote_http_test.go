package cli

import (
	"strings"
	"testing"
)

func TestParseRemoteHTTPResponse(t *testing.T) {
	t.Parallel()

	body, status, err := parseRemoteHTTPResponse(`{"ok":true}` + "\nOVPN_HTTP_STATUS:200")
	if err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if body != `{"ok":true}` || status != 200 {
		t.Fatalf("unexpected parsed response body=%q status=%d", body, status)
	}
}

func TestParseRemoteHTTPResponseKeepsHTTPErrorBody(t *testing.T) {
	t.Parallel()

	body, status, err := parseRemoteHTTPResponse(`{"error":"failed to remove user"}` + "\nOVPN_HTTP_STATUS:500")
	if err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if status != 500 || !strings.Contains(body, "failed to remove user") {
		t.Fatalf("unexpected parsed error body=%q status=%d", body, status)
	}
}

func TestParseRemoteHTTPResponseRejectsMissingStatus(t *testing.T) {
	t.Parallel()

	if _, _, err := parseRemoteHTTPResponse(`{"ok":true}`); err == nil || !strings.Contains(err.Error(), "missing HTTP status marker") {
		t.Fatalf("expected missing marker error, got %v", err)
	}
}

func TestParseRemoteHTTPResponseRejectsInvalidStatus(t *testing.T) {
	t.Parallel()

	if _, _, err := parseRemoteHTTPResponse("body\nOVPN_HTTP_STATUS:not-a-status"); err == nil || !strings.Contains(err.Error(), "invalid HTTP status") {
		t.Fatalf("expected invalid status error, got %v", err)
	}
}

func TestBuildAgentHTTPCommandSendsBearerToken(t *testing.T) {
	t.Parallel()

	get := buildAgentHTTPCommand("GET", "http://127.0.0.1:19000/health", nil)
	if !strings.Contains(get, "OVPN_AGENT_TOKEN=$(sed -n 's/^OVPN_AGENT_TOKEN=//p' /opt/ovpn/.env") {
		t.Fatalf("GET command should source the token from the host .env: %q", get)
	}
	if !strings.Contains(get, `-H "Authorization: Bearer ${OVPN_AGENT_TOKEN}"`) {
		t.Fatalf("GET command should send the bearer header: %q", get)
	}

	post := buildAgentHTTPCommand("POST", "http://127.0.0.1:19000/quota/reset", map[string]string{"email": "a@b"})
	if !strings.Contains(post, `-H "Authorization: Bearer ${OVPN_AGENT_TOKEN}"`) {
		t.Fatalf("POST command should send the bearer header: %q", post)
	}
	if !strings.Contains(post, "cat <<'JSON'") || !strings.Contains(post, `{"email":"a@b"}`) {
		t.Fatalf("POST command should stream JSON payload via stdin: %q", post)
	}
}
