package deploy

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/xtls/xray-core/common/geodata"
	"google.golang.org/protobuf/proto"

	"ovpn/internal/defaults"
	"ovpn/internal/model"
	"ovpn/internal/xraycfg"
)

// Named volumes keep Linux ownership semantics even with Docker Desktop.
func TestBootstrapPreservesSelfSNICertificatePermissions(t *testing.T) {
	if os.Getenv("OVPN_TEST_DOCKER") != "1" {
		t.Skip("set OVPN_TEST_DOCKER=1 to run the Docker regression")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	docker := func(script string, args ...string) (string, error) {
		t.Helper()
		cmd := exec.CommandContext(ctx, "docker", args...)
		cmd.Stdin = strings.NewReader(script)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	mustDocker := func(script string, args ...string) string {
		t.Helper()
		out, err := docker(script, args...)
		if err != nil {
			t.Fatalf("docker %s: %v: %s", args[0], err, out)
		}
		return out
	}
	volume := fmt.Sprintf("ovpn-bootstrap-permissions-%d", time.Now().UnixNano())
	mustDocker("", "volume", "create", volume)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(cleanupCtx, "docker", "volume", "rm", volume).CombinedOutput(); err != nil {
			t.Errorf("remove test volume: %v: %s", err, out)
		}
	})
	alpine := func(script string) string {
		t.Helper()
		return mustDocker(script, "run", "--rm", "-i", "--network", "none", "-v", volume+":/opt", defaults.DefaultAgentImage, "sh", "-s")
	}
	certificate, key := bootstrapTestCertificate(t)
	seed := `set -eu
mkdir -p /opt/ovpn/certs /opt/ovpn/xray /opt/ovpn/logs /opt/ovpn/monitoring/secrets /opt/ovpn-backups/snapshot
printf 'synthetic token\n' > /opt/ovpn/monitoring/secrets/token
printf 'synthetic backup\n' > /opt/ovpn-backups/snapshot/state
chown 65532:65532 /opt/ovpn/logs
chmod 770 /opt/ovpn/logs
chown 1234:2345 /opt/ovpn-backups/snapshot/state
chmod 600 /opt/ovpn/monitoring/secrets/token /opt/ovpn-backups/snapshot/state
# Docker/Compose already exist on the target; sudo only forwards in this root fixture.
printf '#!/bin/sh\nexit 0\n' > /usr/local/bin/docker
printf '#!/bin/sh\nexec "$@"\n' > /usr/local/bin/sudo
chmod 755 /usr/local/bin/docker /usr/local/bin/sudo
`
	files := map[string][]byte{
		"/opt/ovpn/certs/fullchain.pem": certificate,
		"/opt/ovpn/certs/privkey.pem":   key,
	}
	for _, role := range []string{model.ServerRoleVPN, model.ServerRoleProxy} {
		spec := xraycfg.Spec{
			Role: role, Domain: "proxy.example.net", ProxyPreset: model.ProxyPresetRU,
			EnabledProfiles: []string{model.TransportProfileTLSSelfSNIWeb}, SecurityProfile: xraycfg.SecurityProfileOff,
			TLSSelfSNICertFile: "/opt/ovpn/certs/fullchain.pem", TLSSelfSNIKeyFile: "/opt/ovpn/certs/privkey.pem",
			ProxyRelay: &xraycfg.ProxyRelay{Address: "backend.example.net", Port: 443,
				ServiceUUID: "11111111-1111-1111-1111-111111111111", ServerName: "backend.example.net",
				PublicKey: base64.RawURLEncoding.EncodeToString(make([]byte, 32)), ShortID: "abcd1234"},
		}
		raw, err := xraycfg.RenderServerJSON(spec)
		if err != nil {
			t.Fatal(err)
		}
		files["/opt/ovpn/xray/"+role+".json"] = raw
	}
	site, err := proto.Marshal(&geodata.GeoSiteList{Entry: []*geodata.GeoSite{{Code: "RU-AVAILABLE-ONLY-INSIDE"}}})
	if err != nil {
		t.Fatal(err)
	}
	ip, err := proto.Marshal(&geodata.GeoIPList{Entry: []*geodata.GeoIP{
		{Code: "RU", Cidr: []*geodata.CIDR{{Ip: []byte{192, 0, 2, 0}, Prefix: 24}}},
		{Code: "PRIVATE", Cidr: []*geodata.CIDR{{Ip: []byte{10, 0, 0, 0}, Prefix: 8}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	files["/opt/ovpn/xray/geosite.dat"] = site
	files["/opt/ovpn/xray/geoip.dat"] = ip
	for path, data := range files {
		seed += fmt.Sprintf("printf '%%s' %s | base64 -d > %s\n", shellQuote(base64.StdEncoding.EncodeToString(data)), shellQuote(path))
	}
	seed += `chown 0:65532 /opt/ovpn/certs /opt/ovpn/certs/* /opt/ovpn/xray/*.json
chmod 750 /opt/ovpn/certs
chmod 644 /opt/ovpn/certs/fullchain.pem /opt/ovpn/xray/*.dat
chmod 640 /opt/ovpn/certs/privkey.pem /opt/ovpn/xray/*.json
stat -c '%u:%g:%a:%n' /opt/ovpn/certs /opt/ovpn/certs/* /opt/ovpn/xray/*.json /opt/ovpn/logs /opt/ovpn/monitoring/secrets/token /opt/ovpn-backups/snapshot/state > /opt/permissions-before
find /opt/ovpn /opt/ovpn-backups -type f -exec sha256sum {} \; | sort > /opt/content-before
`
	// Run initialization twice: prepared certificates and repeated init must both survive.
	// Bootstrap's dependencies are stubbed in each disposable container, but chown is real.
	bootstrap := seed + "\n" + buildBootstrapCommand() + "\n" + buildBootstrapCommand() + `
stat -c '%u:%g:%a:%n' /opt/ovpn/certs /opt/ovpn/certs/* /opt/ovpn/xray/*.json /opt/ovpn/logs /opt/ovpn/monitoring/secrets/token /opt/ovpn-backups/snapshot/state > /opt/permissions-after
find /opt/ovpn /opt/ovpn-backups -type f -exec sha256sum {} \; | sort > /opt/content-after
diff -u /opt/permissions-before /opt/permissions-after
diff -u /opt/content-before /opt/content-after
`
	alpine("export USER=root\n" + bootstrap)
	image := defaults.DefaultXrayImage("")
	for _, role := range []string{model.ServerRoleVPN, model.ServerRoleProxy} {
		t.Run(role, func(t *testing.T) {
			mustDocker("", "run", "--rm", "--network", "none", "-v", volume+":/opt:ro", "-e", "XRAY_LOCATION_ASSET=/opt/ovpn/xray", image, "run", "-test", "-config", "/opt/ovpn/xray/"+role+".json")
		})
	}
	// Negative control reproduces the original failure entirely inside Linux storage.
	alpine("chown -R root:root /opt/ovpn /opt/ovpn-backups\nchown 0:65532 /opt/ovpn/xray/*.json\n")
	for _, file := range []string{"fullchain.pem", "privkey.pem"} {
		t.Run("reject-inaccessible-"+file, func(t *testing.T) {
			if file == "privkey.pem" {
				// Restore directory traversal while leaving the private key unreadable.
				alpine("chown 0:65532 /opt/ovpn/certs /opt/ovpn/certs/fullchain.pem\n")
			}
			out, err := docker("", "run", "--rm", "--network", "none", "-v", volume+":/opt:ro", image, "run", "-test", "-config", "/opt/ovpn/xray/vpn.json")
			if err == nil || !strings.Contains(out, "permission denied") || !strings.Contains(out, file) {
				t.Fatalf("recursive ownership change must deny Xray access to %s: %v: %s", file, err, out)
			}
		})
	}
}

func bootstrapTestCertificate(t *testing.T) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "proxy.example.net"},
		DNSNames: []string{"proxy.example.net"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}
