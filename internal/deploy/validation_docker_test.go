package deploy

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/xtls/xray-core/common/geodata"
	"google.golang.org/protobuf/proto"

	"ovpn/internal/defaults"
)

// This regression needs a Docker daemon; ordinary unit tests remain offline.
func TestValidateConfigWithDockerPrivateGeodata(t *testing.T) {
	if os.Getenv("OVPN_TEST_DOCKER") != "1" {
		t.Skip("set OVPN_TEST_DOCKER=1 to run the Docker regression")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	image := os.Getenv("OVPN_TEST_XRAY_IMAGE")
	if image == "" {
		image = defaults.DefaultXrayImage("")
	}
	dir := t.TempDir()
	config := filepath.Join(dir, "config.json")
	site := filepath.Join(dir, "geosite.dat")
	ip := filepath.Join(dir, "geoip.dat")
	data, err := proto.Marshal(&geodata.GeoSiteList{Entry: []*geodata.GeoSite{{Code: "CATEGORY-PUBLIC-TRACKER", Domain: []*geodata.Domain{{Type: geodata.Domain_Full, Value: "tracker.example.test"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	ipData, err := proto.Marshal(&geodata.GeoIPList{Entry: []*geodata.GeoIP{{Code: "PRIVATE", Cidr: []*geodata.CIDR{{Ip: []byte{10, 0, 0, 0}, Prefix: 8}}}}})
	if err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string][]byte{
		config: []byte(`{"log":{"loglevel":"none"},"outbounds":[{"tag":"blocked","protocol":"blackhole"}],"routing":{"rules":[{"type":"field","domain":["geosite:category-public-tracker"],"outboundTag":"blocked"},{"type":"field","ip":["geoip:private"],"outboundTag":"blocked"}]}}`),
		site:   data, ip: ipData,
	} {
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mounts := []string{"-v", fmt.Sprintf("%s:/usr/local/share/xray/geosite.dat:ro", site), "-v", fmt.Sprintf("%s:/usr/local/share/xray/geoip.dat:ro", ip)}
	// Keep the config readable for the original command to isolate the geodata failure.
	if err := os.Chmod(config, 0o644); err != nil {
		t.Fatal(err)
	}
	args := []string{"run", "--rm", "-v", config + ":/etc/xray/config.json:ro"}
	args = append(args, mounts...)
	args = append(args, image, "run", "-test", "-config", "/etc/xray/config.json")
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	// Docker Desktop may remap bind-mount ownership and allow the original
	// command. Linux CI must reproduce the permission failure before the fix.
	if err == nil && runtime.GOOS != "linux" {
		t.Log("Docker Desktop allows the original bind mounts; Linux CI checks the failure")
	} else if err == nil || !strings.Contains(string(out), "geosite.dat") || !strings.Contains(string(out), "permission denied") {
		t.Fatalf("expected original geosite permission failure: %v: %s", err, out)
	}
	if err := os.Chmod(config, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateConfigWithDockerAndMounts(ctx, image, config, mounts); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{config, site, ip} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("validation changed source permissions: %s: %o", filepath.Base(path), info.Mode().Perm())
		}
	}
}
