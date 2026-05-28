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
