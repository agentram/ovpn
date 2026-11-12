package util

import (
	"net/url"
	"testing"
)

func TestFileURLSeparatesWindowsDriveFromAuthority(t *testing.T) {
	for _, test := range []struct {
		path    string
		windows bool
		want    string
	}{
		{`C:\Desk state # %\ovpn.db`, true, `file:///C:/Desk%20state%20%23%20%25/ovpn.db`},
		{`/tmp/Desk state # %/ovpn.db`, false, `file:///tmp/Desk%20state%20%23%20%25/ovpn.db`},
		{`/tmp/a\b/ovpn.db`, false, `file:///tmp/a%5Cb/ovpn.db`},
		{`\\node\share\ovpn.db`, true, `file:////node/share/ovpn.db`},
	} {
		u := fileURL(test.path, test.windows)
		if u.String() != test.want {
			t.Fatalf("wrong file URI: %s", u.String())
		}
		parsed, err := url.Parse(u.String())
		if err != nil || parsed.Host != "" || parsed.Scheme != "file" {
			t.Fatal("filesystem path escaped into URI authority", err)
		}
	}
}
