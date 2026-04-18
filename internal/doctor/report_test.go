package doctor

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSummarize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		checks []Check
		want   Status
	}{
		{name: "empty", checks: nil, want: StatusSkip},
		{name: "pass", checks: []Check{{Status: StatusPass}}, want: StatusPass},
		{name: "warn", checks: []Check{{Status: StatusPass}, {Status: StatusWarn}}, want: StatusWarn},
		{name: "fail", checks: []Check{{Status: StatusWarn}, {Status: StatusFail}}, want: StatusFail},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Summarize(tc.checks)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFormatHuman(t *testing.T) {
	t.Parallel()

	r := NewReport("main")
	r.Add(Check{Name: "SSH", Status: StatusPass, Message: "ok", Details: []string{"hostname=vps"}})
	r.Add(Check{Name: "Xray", Status: StatusFail, Message: "config invalid", Hint: "run ovpn deploy main"})
	r.Finalize()
	out := FormatHuman(r, false)
	if !strings.Contains(out, "[PASS] SSH: ok") {
		t.Fatalf("expected pass line in output, got: %s", out)
	}
	if !strings.Contains(out, "[FAIL] Xray: config invalid") {
		t.Fatalf("expected fail line in output, got: %s", out)
	}
	if !strings.Contains(out, "hint: run ovpn deploy main") {
		t.Fatalf("expected hint in output, got: %s", out)
	}
}

func TestReportJSON(t *testing.T) {
	t.Parallel()

	r := NewReport("main")
	r.Add(Check{Name: "SSH", Status: StatusPass, Message: "ok"})
	r.Finalize()

	raw, err := r.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal json: %v", err)
	}
	if got["server"] != "main" {
		t.Fatalf("unexpected server: %#v", got["server"])
	}
	if got["overall_status"] != string(StatusPass) {
		t.Fatalf("unexpected overall status: %#v", got["overall_status"])
	}
}
