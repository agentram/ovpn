package doctor

import "testing"

func TestParseKV(t *testing.T) {
	t.Parallel()

	raw := "A=1\nB = two words\ninvalid\n=bad\n"
	got := ParseKV(raw)
	if got["A"] != "1" {
		t.Fatalf("A mismatch: %q", got["A"])
	}
	if got["B"] != "two words" {
		t.Fatalf("B mismatch: %q", got["B"])
	}
	if _, ok := got["invalid"]; ok {
		t.Fatalf("unexpected key parsed")
	}
}

func TestParseComposePSArray(t *testing.T) {
	t.Parallel()

	raw := `[{"Service":"xray","Name":"ovpn-xray","State":"running","Status":"Up 5m"},{"Service":"ovpn-agent","Name":"ovpn-agent","State":"exited","Status":"Exited (1)"}]`
	got, err := ParseComposePS(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d items", len(got))
	}
	if got[0].Service != "xray" || got[1].Service != "ovpn-agent" {
		t.Fatalf("unexpected parse result: %#v", got)
	}
}

func TestParseComposePSJSONLines(t *testing.T) {
	t.Parallel()

	raw := `{"Service":"xray","Name":"ovpn-xray","State":"running"}
{"Name":"ovpn-agent","State":"running"}`
	got, err := ParseComposePS(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d items", len(got))
	}
	if got[1].Service != "ovpn-agent" {
		t.Fatalf("expected fallback service name from Name, got: %#v", got[1])
	}
}
