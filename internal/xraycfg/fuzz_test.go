package xraycfg

import (
	"strings"
	"testing"
)

func FuzzURLEscapeLabel(f *testing.F) {
	f.Add("ovpn user")
	f.Add("hello#world")
	f.Add("already%20escaped")

	f.Fuzz(func(t *testing.T, in string) {
		out := urlEscapeLabel(in)
		if strings.Contains(out, " ") {
			t.Fatalf("escaped label contains space: %q", out)
		}
		if strings.Contains(out, "#") {
			t.Fatalf("escaped label contains #: %q", out)
		}
	})
}
