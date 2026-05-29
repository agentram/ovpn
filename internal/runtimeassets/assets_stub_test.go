//go:build !runtimeassets

package runtimeassets

import (
	"errors"
	"testing"
)

func TestStubOpenReportsNotEmbedded(t *testing.T) {
	if LinuxGOOS != "linux" {
		t.Fatalf("LinuxGOOS=%q", LinuxGOOS)
	}
	file, asset, err := Open("ovpn-agent", "amd64")
	if file != nil || asset.Path != "" || !errors.Is(err, ErrNotEmbedded) {
		t.Fatalf("expected not embedded stub response, file=%v asset=%+v err=%v", file, asset, err)
	}
}
