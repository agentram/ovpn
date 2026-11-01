package deploy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ovpn/internal/ssh"
)

func fakeValidationDocker(t *testing.T, output string) string {
	t.Helper()
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$OVPN_TEST_DOCKER_ARGS\"\nprintf '%s\\n' \"$OVPN_TEST_DOCKER_OUTPUT\"\nexit \"$OVPN_TEST_DOCKER_EXIT\"\n"
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("OVPN_TEST_DOCKER_ARGS", argsFile)
	t.Setenv("OVPN_TEST_DOCKER_OUTPUT", output)
	t.Setenv("OVPN_TEST_DOCKER_EXIT", "0")
	return argsFile
}

func TestValidateConfigWithDockerUsesLocalIdentity(t *testing.T) {
	argsFile := fakeValidationDocker(t, "")
	mounts := []string{"-v", "/cache with spaces/geosite.dat:/usr/local/share/xray/geosite.dat:ro", "-v", "/cache/geoip.dat:/usr/local/share/xray/geoip.dat:ro", "-v", "/private/certs:/etc/xray/certs:ro"}
	if err := ValidateConfigWithDockerAndMounts(context.Background(), "test/xray:1", "/private/config.json", mounts); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"run", "--rm", "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), "-v", "/private/config.json:/etc/xray/config.json:ro"}
	want = append(want, mounts...)
	want = append(want, "test/xray:1", "run", "-test", "-config", "/etc/xray/config.json")
	if string(raw) != strings.Join(want, "\n")+"\n" {
		t.Fatalf("unexpected docker arguments: %s", raw)
	}
}

func TestValidateConfigWithDockerErrorHints(t *testing.T) {
	for _, tc := range []struct{ name, output, hint string }{
		{"geosite permissions", "failed to open geosite.dat: permission denied", "check read permissions"},
		{"geoip permissions", "open geoip.dat: permission denied", "check read permissions"},
		{"missing geosite", "failed to open geosite.dat: no such file", "OVPN_SECURITY_PROFILE=off"},
		{"other error", "invalid config", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeValidationDocker(t, tc.output)
			t.Setenv("OVPN_TEST_DOCKER_EXIT", "23")
			err := ValidateConfigWithDocker(context.Background(), "test/xray:1", "/config.json")
			if err == nil || !strings.Contains(err.Error(), tc.output) {
				t.Fatalf("expected original error, got %v", err)
			}
			if tc.hint != "" && !strings.Contains(err.Error(), tc.hint) {
				t.Fatalf("expected hint %q, got %v", tc.hint, err)
			}
			if tc.hint == "" && strings.Contains(err.Error(), "; hint:") {
				t.Fatalf("unexpected hint: %v", err)
			}
			if strings.Contains(tc.name, "permissions") && strings.Contains(err.Error(), "OVPN_SECURITY_PROFILE=off") {
				t.Fatalf("permission failure must not suggest disabling security: %v", err)
			}
		})
	}
}

func TestDeployRemoteGeodataPermissionHint(t *testing.T) {
	r := &failingRunner{failOn: "run -test -config /etc/xray/config.json", err: fmt.Errorf("open geosite.dat: permission denied")}
	err := DeployRemote(context.Background(), r, ssh.Config{Host: "example-host"})
	if err == nil || !strings.Contains(err.Error(), "check geodata read permissions") || strings.Contains(err.Error(), "OVPN_SECURITY_PROFILE=off") {
		t.Fatalf("unexpected permission hint: %v", err)
	}
}

func TestValidateConfigWithDockerRequiresImage(t *testing.T) {
	err := ValidateConfigWithDocker(context.Background(), "", "/config.json")
	if err == nil || err.Error() != "xray image is required" {
		t.Fatalf("expected missing image error, got %v", err)
	}
}

func TestDeployRemoteSelfSNICertificatePermissionHint(t *testing.T) {
	for _, path := range []string{"/etc/xray/certs/fullchain.pem", "/etc/xray/certs/privkey.pem"} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			r := &failingRunner{failOn: "run -test -config /etc/xray/config.json", err: fmt.Errorf("failed to parse certificate > open %s: permission denied", path)}
			err := DeployRemote(context.Background(), r, ssh.Config{Host: "example-host"})
			if err == nil {
				t.Fatal("expected certificate validation failure")
			}
			for _, want := range []string{path, "ovpn_camouflage_enabled=true", "root:65532", "0750", "0640", "retry init or deploy"} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("missing recovery detail %q: %v", want, err)
				}
			}
			if len(r.execCmds) != 4 {
				t.Fatal("certificate failure must stop before applying or restarting the runtime")
			}
			if strings.Contains(err.Error(), "OVPN_SECURITY_PROFILE=off") {
				t.Fatal("certificate permissions must not suggest disabling security")
			}
		})
	}
}

func TestSelfSNICertificatePermissionHintIsSpecific(t *testing.T) {
	for _, message := range []string{
		"open /etc/xray/certs/fullchain.pem: no such file or directory",
		"open /etc/xray/config.json: permission denied",
		"open geosite.dat: permission denied",
		"failed to parse certificate: invalid PEM",
	} {
		if isXraySelfSNICertificatePermissionError(message) {
			t.Fatalf("unrelated error classified as a certificate permission failure: %s", message)
		}
	}
}
