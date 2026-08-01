package cli

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"ovpn/internal/model"
	"ovpn/internal/ssh"
)

const (
	testVLESSClientEncryption = "mlkem768x25519plus.native.0rtt.client-secret"
	testVLESSServerDecryption = "mlkem768x25519plus.native.600s.server-secret"
)

func TestParseVLESSEncryptionOutputSelectsMLKEMNativePair(t *testing.T) {
	t.Parallel()

	raw := []byte(`Authentication: X25519, Classical
"decryption": "x25519mlkem768.native.600s.wrong-server"
"encryption": "x25519mlkem768.native.0rtt.wrong-client"

Authentication: ML-KEM-768, Post-Quantum
"decryption": "` + testVLESSServerDecryption + `"
"encryption": "` + testVLESSClientEncryption + `"
`)
	client, server, err := parseVLESSEncryptionOutput(raw)
	if err != nil {
		t.Fatalf("parse vlessenc output: %v", err)
	}
	if client != testVLESSClientEncryption || server != testVLESSServerDecryption {
		t.Fatalf("unexpected pair: client=%q server=%q", client, server)
	}
}

func TestParseVLESSEncryptionOutputAcceptsLongNonSecretLines(t *testing.T) {
	t.Parallel()

	raw := "Authentication: ML-KEM-768, Post-Quantum\n" +
		strings.Repeat("x", 128*1024) + "\n" +
		"\"decryption\": \"" + testVLESSServerDecryption + "\"\n" +
		"\"encryption\": \"" + testVLESSClientEncryption + "\"\n"
	client, server, err := parseVLESSEncryptionOutput([]byte(raw))
	if err != nil {
		t.Fatalf("parse vlessenc output: %v", err)
	}
	if client != testVLESSClientEncryption || server != testVLESSServerDecryption {
		t.Fatalf("unexpected pair: client=%q server=%q", client, server)
	}
}

func TestParseVLESSEncryptionOutputRejectsUnexpectedShapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "missing section",
			raw:  `Authentication: X25519, Classical`,
			want: "did not return an ML-KEM-768 section",
		},
		{
			name: "missing encryption",
			raw:  "Authentication: ML-KEM-768, Post-Quantum\n\"decryption\": \"" + testVLESSServerDecryption + "\"\n",
			want: "expected ML-KEM-768 native",
		},
		{
			name: "duplicate encryption",
			raw: "Authentication: ML-KEM-768, Post-Quantum\n" +
				"\"decryption\": \"" + testVLESSServerDecryption + "\"\n" +
				"\"encryption\": \"" + testVLESSClientEncryption + "\"\n" +
				"\"encryption\": \"" + testVLESSClientEncryption + "\"\n",
			want: "duplicate encryption",
		},
		{
			name: "unknown quoted field",
			raw: "Authentication: ML-KEM-768, Post-Quantum\n" +
				"\"decryption\": \"" + testVLESSServerDecryption + "\"\n" +
				"\"ticket\": \"unexpected\"\n" +
				"\"encryption\": \"" + testVLESSClientEncryption + "\"\n",
			want: "unexpected field",
		},
		{
			name: "wrong mode",
			raw: "Authentication: ML-KEM-768, Post-Quantum\n" +
				"\"decryption\": \"mlkem768x25519plus.native.60s.server\"\n" +
				"\"encryption\": \"" + testVLESSClientEncryption + "\"\n",
			want: "expected ML-KEM-768 native",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := parseVLESSEncryptionOutput([]byte(tt.raw))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestParseVLESSEncryptionOutputDoesNotEchoUnexpectedValues(t *testing.T) {
	t.Parallel()

	const unexpectedSecret = "must-not-appear-in-cli-output"
	raw := "Authentication: ML-KEM-768, Post-Quantum\n" +
		"\"unexpected\": \"" + unexpectedSecret + "\"\n"
	_, _, err := parseVLESSEncryptionOutput([]byte(raw))
	if err == nil || !strings.Contains(err.Error(), "unexpected field") {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(err.Error(), unexpectedSecret) {
		t.Fatalf("error leaked unexpected value: %v", err)
	}
}

func TestGenerateVLESSEncryptionPairUsesPinnedImageOnTargetHost(t *testing.T) {
	t.Parallel()

	target := model.Server{Name: "vpn-a", Host: "192.0.2.10", SSHUser: "root", SSHPort: 22, XrayVersion: "26.7.28"}
	app := &App{ctx: context.Background()}
	app.remoteExecHook = func(cfg ssh.Config, timeout time.Duration, command string) (ssh.Result, error) {
		if cfg.Host != target.Host || cfg.User != target.SSHUser || cfg.Port != target.SSHPort {
			t.Fatalf("unexpected target SSH config: %#v", cfg)
		}
		if timeout != 2*time.Minute {
			t.Fatalf("timeout = %s, want %s", timeout, 2*time.Minute)
		}
		for _, want := range []string{
			"image='ghcr.io/xtls/xray-core:26.7.28'",
			`sudo -n docker image inspect "$image"`,
			`sudo -n docker pull "$image"`,
			`sudo -n docker run --rm "$image" vlessenc`,
		} {
			if !strings.Contains(command, want) {
				t.Fatalf("command %q does not contain %q", command, want)
			}
		}
		return ssh.Result{Stdout: "Authentication: ML-KEM-768, Post-Quantum\n" +
			"\"decryption\": \"" + testVLESSServerDecryption + "\"\n" +
			"\"encryption\": \"" + testVLESSClientEncryption + "\"\n"}, nil
	}

	client, server, err := app.generateVLESSEncryptionPair(target)
	if err != nil {
		t.Fatalf("generate pair: %v", err)
	}
	if client != testVLESSClientEncryption || server != testVLESSServerDecryption {
		t.Fatalf("unexpected generated pair")
	}
}

func TestGenerateVLESSEncryptionPairReportsTargetDockerFailureWithoutOutput(t *testing.T) {
	t.Parallel()

	target := model.Server{Name: "vpn-a", Host: "192.0.2.10", SSHUser: "root", SSHPort: 22, XrayVersion: "26.7.28"}
	app := &App{ctx: context.Background()}
	app.remoteExecHook = func(ssh.Config, time.Duration, string) (ssh.Result, error) {
		return ssh.Result{Stdout: "sensitive command output"}, errors.New("daemon unavailable")
	}
	_, _, err := app.generateVLESSEncryptionPair(target)
	if err == nil || !strings.Contains(err.Error(), "target host") {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(err.Error(), "sensitive command output") {
		t.Fatalf("error leaked command output: %v", err)
	}
}

func TestEnsureVLESSEncryptionForServerGeneratesOnceAndSharesPair(t *testing.T) {
	t.Parallel()

	app := newTestAppWithServer(t, false)
	first, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatalf("get first server: %v", err)
	}
	second := &model.Server{
		Name:              "second",
		Host:              "192.0.2.2",
		Domain:            "second.example.com",
		SSHUser:           "root",
		SSHPort:           22,
		XrayVersion:       "26.7.28",
		RealityPrivateKey: "private",
		RealityPublicKey:  "public",
		RealityShortIDs:   "abcd1234",
		RealityServerName: "www.example.com",
		RealityTarget:     "www.example.com:443",
		Enabled:           true,
	}
	if err := app.store.AddServer(app.ctx, second); err != nil {
		t.Fatalf("add second server: %v", err)
	}

	calls := 0
	app.remoteExecHook = func(ssh.Config, time.Duration, string) (ssh.Result, error) {
		calls++
		return ssh.Result{Stdout: "Authentication: ML-KEM-768, Post-Quantum\n" +
			"\"decryption\": \"" + testVLESSServerDecryption + "\"\n" +
			"\"encryption\": \"" + testVLESSClientEncryption + "\"\n"}, nil
	}
	if err := app.ensureVLESSEncryptionForServer(first); err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	if err := app.ensureVLESSEncryptionForServer(second); err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	if calls != 1 {
		t.Fatalf("generator calls = %d, want 1", calls)
	}

	for _, name := range []string{"main", "second"} {
		srv, err := app.store.GetServerByName(app.ctx, name)
		if err != nil {
			t.Fatalf("get %s: %v", name, err)
		}
		if srv.VLESSClientEncryption != testVLESSClientEncryption ||
			srv.VLESSServerDecryption != testVLESSServerDecryption {
			t.Fatalf("%s did not inherit cluster pair", name)
		}
	}
}

func TestEnsureVLESSEncryptionParityRejectsMismatch(t *testing.T) {
	t.Parallel()

	err := ensureVLESSEncryptionParityForServers([]model.Server{
		{
			Name:                  "one",
			VLESSClientEncryption: testVLESSClientEncryption,
			VLESSServerDecryption: testVLESSServerDecryption,
		},
		{
			Name:                  "two",
			VLESSClientEncryption: testVLESSClientEncryption + "-other",
			VLESSServerDecryption: testVLESSServerDecryption,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "server two differs from one") {
		t.Fatalf("unexpected parity error: %v", err)
	}
}

func TestEnsureVLESSEncryptionParityIncludesDisabledVPNServers(t *testing.T) {
	t.Parallel()

	app := newTestAppWithServer(t, false)
	main, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatalf("get main server: %v", err)
	}
	main.VLESSClientEncryption = testVLESSClientEncryption
	main.VLESSServerDecryption = testVLESSServerDecryption
	if err := app.store.UpdateServer(app.ctx, main); err != nil {
		t.Fatalf("update main server: %v", err)
	}

	disabled := *main
	disabled.ID = 0
	disabled.Name = "disabled"
	disabled.Host = "192.0.2.50"
	disabled.Domain = "disabled.example.com"
	disabled.Enabled = false
	disabled.VLESSClientEncryption += "-other"
	if err := app.store.AddServer(app.ctx, &disabled); err != nil {
		t.Fatalf("add disabled server: %v", err)
	}

	err = app.ensureVLESSEncryptionParity()
	if err == nil || !strings.Contains(err.Error(), "server disabled differs from main") {
		t.Fatalf("unexpected parity error: %v", err)
	}
}
