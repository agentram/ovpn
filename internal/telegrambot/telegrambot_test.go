package telegrambot

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseIDSetCSV(t *testing.T) {
	t.Parallel()

	got, err := ParseIDSetCSV(" 10,20,10 ")
	if err != nil {
		t.Fatalf("parse id set: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 unique ids, got %d", len(got))
	}
	if _, ok := got[10]; !ok {
		t.Fatalf("missing id 10")
	}
	if _, ok := got[20]; !ok {
		t.Fatalf("missing id 20")
	}
}

func TestParseIDSetCSVRejectsInvalidValue(t *testing.T) {
	t.Parallel()

	if _, err := ParseIDSetCSV("10,bad"); err == nil {
		t.Fatalf("expected parse error")
	}
}

func TestParseIDSliceCSVSorted(t *testing.T) {
	t.Parallel()

	got, err := ParseIDSliceCSV("20,10,20")
	if err != nil {
		t.Fatalf("parse id slice: %v", err)
	}
	if len(got) != 2 || got[0] != 10 || got[1] != 20 {
		t.Fatalf("unexpected id slice: %#v", got)
	}
}

func TestIsAllowed(t *testing.T) {
	t.Parallel()

	allowedChats := map[int64]struct{}{100: {}}
	allowedUsers := map[int64]struct{}{200: {}}
	if !IsAllowed(100, 1, allowedChats, allowedUsers) {
		t.Fatalf("expected allowed by chat")
	}
	if !IsAllowed(999, 200, allowedChats, allowedUsers) {
		t.Fatalf("expected allowed by user")
	}
	if IsAllowed(999, 999, allowedChats, allowedUsers) {
		t.Fatalf("unexpected allowed for unknown ids")
	}
	if IsAllowed(1, 1, nil, nil) {
		t.Fatalf("unexpected allowed when allowlist empty")
	}
}

func TestParseCommand(t *testing.T) {
	t.Parallel()

	cmd, args, err := ParseCommand("/STATUS@OvPn_Bot extra")
	if err != nil {
		t.Fatalf("parse command: %v", err)
	}
	if cmd != "/status" {
		t.Fatalf("unexpected command: %q", cmd)
	}
	if len(args) != 1 || args[0] != "extra" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestParseCommandRejectsPlainText(t *testing.T) {
	t.Parallel()

	if _, _, err := ParseCommand("hello"); err == nil {
		t.Fatalf("expected non-command error")
	}
}

func TestRenderNotifyMessage(t *testing.T) {
	t.Parallel()

	text := RenderNotifyMessage(NotifyEvent{
		Event:    "deploy",
		Status:   "success",
		Severity: "info",
		Source:   "ovpn-cli",
		Message:  "done",
	})
	if !strings.Contains(text, "[SUCCESS] deploy") {
		t.Fatalf("missing header: %q", text)
	}
	if !strings.Contains(text, "source: ovpn-cli | severity: info") {
		t.Fatalf("missing source/severity: %q", text)
	}
}

func TestRenderAlertmanagerMessage(t *testing.T) {
	t.Parallel()

	raw := `{
		"status":"firing",
		"groupLabels":{"alertname":"HighUsage"},
		"alerts":[{"labels":{"job":"ovpn_agent","instance":"ovpn-agent:9090"}}],
		"commonLabels":{"severity":"warning"},
		"commonAnnotations":{"summary":"summary text"},
		"externalURL":"http://eebcaa8aac9c:9093"
	}`
	var payload AlertmanagerWebhook
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	text := RenderAlertmanagerMessage(payload)
	if !strings.Contains(text, "[ALERT FIRING] HighUsage") {
		t.Fatalf("missing alert header: %q", text)
	}
	if !strings.Contains(text, "severity: warning") {
		t.Fatalf("missing severity: %q", text)
	}
	if !strings.Contains(text, "source: ovpn_agent (ovpn-agent:9090)") {
		t.Fatalf("missing source: %q", text)
	}
	if !strings.Contains(text, "summary: summary text") {
		t.Fatalf("missing summary: %q", text)
	}
	if !strings.Contains(text, "alertmanager: http://alertmanager:9093") {
		t.Fatalf("expected normalized alertmanager URL, got: %q", text)
	}
}

func TestRenderAlertmanagerMessageExpirySoonFriendly(t *testing.T) {
	t.Parallel()

	raw := `{
		"status":"firing",
		"groupLabels":{"alertname":"OVPNUserExpirySoon"},
		"alerts":[{"labels":{"job":"ovpn_agent","instance":"ovpn-agent:9090","email":"sample-user@node-a","expiry_date":"2026-04-23"}}],
		"commonLabels":{"severity":"warning","alertname":"OVPNUserExpirySoon","email":"sample-user@node-a","expiry_date":"2026-04-23"},
		"commonAnnotations":{"summary":"User access expires soon","description":"sample-user@node-a expires on 2026-04-23 UTC. Access remains active until the end of that day."},
		"externalURL":"http://alertmanager:9093"
	}`
	var payload AlertmanagerWebhook
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	text := RenderAlertmanagerMessage(payload)
	for _, want := range []string{
		"User Expiry Reminder",
		"User: sample-user",
		"Server: node-a",
		"Identity: sample-user@node-a",
		"Expires on: 2026-04-23 UTC",
		"Access remains active until the end of that day.",
		"Action: extend the expiry date if this user should keep access.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in output, got: %q", want, text)
		}
	}
	if strings.Contains(text, "[ALERT FIRING]") {
		t.Fatalf("did not expect raw alert header in friendly expiry message: %q", text)
	}
}
