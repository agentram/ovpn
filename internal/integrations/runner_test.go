package integrations

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckerProjectionRejectsMixedOrInvalidSeries(t *testing.T) {
	valid := `# TYPE xray_proxy_status gauge
xray_proxy_status{name="opaque",stable_id="stable",address="private-marker"} 1
# TYPE xray_proxy_latency_ms gauge
xray_proxy_latency_ms{name="opaque",stable_id="stable",address="private-marker"} 42
`
	passed, ms, err := ParseChecker([]byte(valid), "opaque")
	if err != nil || !passed || ms != 42 {
		t.Fatal("valid metrics refused", err)
	}
	for _, bad := range []string{strings.Replace(valid, "stable_id=\"stable\"", "stable_id=\"other\"", 1), strings.Replace(valid, " 42", " NaN", 1), strings.Replace(valid, " 1\n", " 2\n", 1), valid + `xray_proxy_status{name="other",stable_id="stable"} 1` + "\n", strings.Repeat("x", 65537)} {
		if _, _, err := ParseChecker([]byte(bad), "opaque"); err == nil {
			t.Fatal("invalid or mismatched metrics accepted")
		}
	}
}

func TestKnifeProjectionDropsPrivateMetadataAndRequiresExactInput(t *testing.T) {
	link := "vless://synthetic-credential@node.example.net:443"
	raw := []byte(fmt.Sprintf(`{"link":%q,"status":"passed","code":204,"ttfb":31,"reason":"private-marker","protocol":{"address":"private-marker"},"ip":"private-marker"}`, link))
	result, err := ParseKnife(raw, link)
	if err != nil || result.Outcome != "passed" || result.TTFBMS != 31 {
		t.Fatal("valid result rejected", err)
	}
	public, _ := json.Marshal(result)
	if strings.Contains(string(public), "private-marker") || strings.Contains(string(public), "synthetic-credential") {
		t.Fatal("provider secrets escaped projection")
	}
	if _, err := ParseKnife(raw, link+"changed"); err == nil {
		t.Fatal("result rebound to another identity")
	}
	if _, err := ParseKnife(append(raw, raw...), link); err == nil {
		t.Fatal("multiple results accepted for one input")
	}
	broken := bytes.Replace(raw, []byte(`"passed"`), []byte(`"broken"`), 1)
	result, err = ParseKnife(broken, link)
	if err != nil || result.Outcome != "observer_error" || result.FailureClass != "observer" {
		t.Fatal("unsupported tool profile labelled node failure")
	}
	publicEndpoint := bytes.Replace(broken, []byte(`"reason":"private-marker"`),
		[]byte(`"reason":"infra/conf: vless without TLS or other encryption is prohibited unless the server address is a private IP or domain: private-marker"`), 1)
	result, err = ParseKnife(publicEndpoint, link)
	public, _ = json.Marshal(result)
	if err != nil || result.Code != "TOOL_REQUIRES_ENCRYPTION" || result.FailureClass != "observer" || bytes.Contains(public, []byte("private-marker")) {
		t.Fatal("public endpoint restriction did not produce a safe compatibility result")
	}
}

func TestPinnedManifestAndArchiveExtractionCannotWritePaths(t *testing.T) {
	for _, engine := range []string{"checker", "knife"} {
		for _, goos := range []string{"linux", "darwin", "windows"} {
			for _, arch := range []string{"amd64", "arm64"} {
				spec, url, err := archiveFor(engine, goos, arch)
				if err != nil || len(spec.SHA256) != 64 || !strings.HasPrefix(url, "https://github.com/") {
					t.Fatal("missing pinned archive")
				}
			}
		}
	}
	if _, _, err := archiveFor("other", "linux", "amd64"); err == nil {
		t.Fatal("arbitrary provider accepted")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "tool.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for _, name := range []string{"../../escape", "sub/xray-knife"} {
		entry, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = entry.Write([]byte("fixture"))
	}
	_ = z.Close()
	_ = f.Close()
	dest := filepath.Join(dir, "selected")
	if err := extractBinary(path, dest, "xray-knife"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != "fixture" {
		t.Fatal("wrong archive member")
	}
	if _, err := os.Stat(filepath.Join(dir, "escape")); !os.IsNotExist(err) {
		t.Fatal("extracted arbitrary path")
	}
}

func TestHashAndEnvironmentBoundary(t *testing.T) {
	if _, err := Verify(context.Background(), Tool{Engine: "checker", Path: "relative", SHA256: strings.Repeat("0", 64)}); err == nil {
		t.Fatal("relative executable accepted")
	}
	t.Setenv("OVPN_SECRET_KEY", "private-marker")
	t.Setenv("SSH_AUTH_SOCK", "private-marker")
	t.Setenv("HTTP_PROXY", "private-marker")
	if strings.Contains(strings.Join(environment(t.TempDir()), "\n"), "private-marker") {
		t.Fatal("control credentials passed to a tool")
	}
}
