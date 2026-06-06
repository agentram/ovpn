package diagnostics

import "testing"

func TestParseAccessLineAcceptedIPv4Domain(t *testing.T) {
	line := `2026/06/05 12:34:56.965413 from 198.51.100.7:48124 accepted tcp:api.telegram.org:443 [vless-reality >> direct] email: arr-1@global`
	ev, ok := ParseAccessLine(line)
	if !ok {
		t.Fatalf("expected parse success")
	}
	if ev.Email != "arr-1@global" || ev.Result != "accepted" {
		t.Fatalf("unexpected event identity: %+v", ev)
	}
	if ev.Timestamp.Nanosecond() != 965413000 {
		t.Fatalf("unexpected timestamp precision: %s", ev.Timestamp.Format("15:04:05.999999"))
	}
	if ev.SourceNetwork != "198.51.100.0/24" {
		t.Fatalf("unexpected source network: %q", ev.SourceNetwork)
	}
	if ev.Destination != "api.telegram.org" || ev.DestinationPort != 443 || ev.DestinationFamily != "domain" {
		t.Fatalf("unexpected destination: %+v", ev)
	}
}

func TestParseAccessLineRejectedIPv6(t *testing.T) {
	line := `2026/06/05 12:34:56 [2001:db8:abcd:12::99]:52000 rejected tcp:[2a00:1450:400f:80c::200e]:443 [vless-reality] email: alice@global`
	ev, ok := ParseAccessLine(line)
	if !ok {
		t.Fatalf("expected parse success")
	}
	if ev.Result != "rejected" || ev.DestinationFamily != "ipv6" || ev.DestinationPort != 443 {
		t.Fatalf("unexpected event: %+v", ev)
	}
	if ev.SourceNetwork != "2001:db8:abcd::/56" {
		t.Fatalf("unexpected source network: %q", ev.SourceNetwork)
	}
}

func TestParseAccessLineMissingEmailAndMalformed(t *testing.T) {
	if _, ok := ParseAccessLine(`2026/06/05 12:34:56 198.51.100.7:48124 accepted tcp:example.org:443`); ok {
		t.Fatalf("missing email should not parse")
	}
	if _, ok := ParseAccessLine(`not an xray access line`); ok {
		t.Fatalf("malformed line should not parse")
	}
}

func TestMaskSourceNetwork(t *testing.T) {
	if got := MaskSourceNetwork("203.0.113.44:12345"); got != "203.0.113.0/24" {
		t.Fatalf("unexpected IPv4 mask: %q", got)
	}
	if got := MaskSourceNetwork("[2001:db8:abcd:1201::1]:12345"); got != "2001:db8:abcd:1200::/56" {
		t.Fatalf("unexpected IPv6 mask: %q", got)
	}
}
