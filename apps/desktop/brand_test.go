package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestNativeIconsTrackTheSharedBrandSVG(t *testing.T) {
	source, err := os.ReadFile("frontend/public/brand/ovpn.svg")
	if err != nil {
		t.Fatal(err)
	}
	digest, err := os.ReadFile("assets/brand.sha256")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(digest)) != fmt.Sprintf("%x", sha256.Sum256(bytes.ReplaceAll(source, []byte("\r\n"), []byte("\n")))) {
		t.Fatal("regenerate native icons with scripts/build_desktop_icons.py")
	}
	for _, file := range []string{"assets/icon.png", "assets/icon.ico", "assets/icon.icns"} {
		info, err := os.Stat(file)
		if err != nil || info.Size() == 0 {
			t.Fatal("missing native brand asset", file)
		}
	}
}
