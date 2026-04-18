package xrayapi

import "testing"

func TestParseUserCounters(t *testing.T) {
	t.Parallel()

	in := map[string]int64{
		"user>>>alice@example.com>>>traffic>>>uplink":   100,
		"user>>>alice@example.com>>>traffic>>>downlink": 200,
		"user>>>bob@example.com>>>traffic>>>uplink":     50,
		"outbound>>>foo>>>traffic>>>uplink":             999,
	}
	got := ParseUserCounters(in)
	if len(got) != 2 {
		t.Fatalf("expected 2 users, got %d", len(got))
	}
	if got["alice@example.com"].Uplink != 100 || got["alice@example.com"].Downlink != 200 {
		t.Fatalf("unexpected alice counters: %+v", got["alice@example.com"])
	}
	if got["bob@example.com"].Uplink != 50 || got["bob@example.com"].Downlink != 0 {
		t.Fatalf("unexpected bob counters: %+v", got["bob@example.com"])
	}
}

func TestAccountForInboundSetsVisionFlowForVLESSReality(t *testing.T) {
	t.Parallel()

	acc := accountForInbound("vless-reality", "u-1")
	if acc.Id != "u-1" {
		t.Fatalf("unexpected id: %q", acc.Id)
	}
	if acc.Flow != defaultVLESSFlow {
		t.Fatalf("unexpected flow: %q", acc.Flow)
	}
}

func TestAccountForInboundSetsVisionFlowWhenTagEmpty(t *testing.T) {
	t.Parallel()

	acc := accountForInbound("", "u-2")
	if acc.Flow != defaultVLESSFlow {
		t.Fatalf("unexpected flow for empty tag: %q", acc.Flow)
	}
}
