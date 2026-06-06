package util

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilesystemJSONAndHashHelpers(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "nested")
	if err := EnsureDir(dir); err != nil {
		t.Fatalf("ensure dir: %v", err)
	}
	path := filepath.Join(dir, "data.txt")
	if err := os.WriteFile(path, []byte("abc"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	sum, err := SHA256File(path)
	if err != nil {
		t.Fatalf("sha file: %v", err)
	}
	if sum != SHA256Bytes([]byte("abc")) {
		t.Fatalf("file hash mismatch: %s", sum)
	}
	if got := PrettyJSON(map[string]string{"a": "b"}); !strings.Contains(got, `"a": "b"`) {
		t.Fatalf("unexpected pretty JSON: %s", got)
	}
	if got := PrettyJSON(func() {}); got != "{}" {
		t.Fatalf("expected marshal fallback, got %s", got)
	}
	if HomeDir() == "" || !strings.HasSuffix(DefaultDataDir(), ".ovpn") {
		t.Fatalf("unexpected home/default data dir: home=%q data=%q", HomeDir(), DefaultDataDir())
	}
	if NowUTC().Location().String() != "UTC" {
		t.Fatalf("expected UTC time")
	}
	if got := JoinCSV(nil); got != "" {
		t.Fatalf("empty JoinCSV got %q", got)
	}
	if got := ParseCSV(" "); got != nil {
		t.Fatalf("blank ParseCSV got %+v", got)
	}
}
