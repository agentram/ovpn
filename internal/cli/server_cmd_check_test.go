package cli

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"ovpn/internal/ssh"
	"strconv"
	"testing"
	"time"

	"github.com/spf13/cobra"
	cryptossh "golang.org/x/crypto/ssh"
)

func TestHostInspectionPinsOnlyTheExplicitlyReviewedKey(t *testing.T) {
	if _, e := os.Stat("/usr/bin/ssh-keyscan"); e != nil {
		t.Skip("requires OpenSSH keyscan")
	}
	_, private, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	signer, e := cryptossh.NewSignerFromKey(private)
	if e != nil {
		t.Fatal(e)
	}
	config := &cryptossh.ServerConfig{NoClientAuth: true}
	config.AddHostKey(signer)
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	go func() {
		for {
			connection, e := listener.Accept()
			if e != nil {
				return
			}
			go func() {
				defer connection.Close()
				_ = connection.SetDeadline(time.Now().Add(3 * time.Second))
				server, _, _, e := cryptossh.NewServerConn(connection, config)
				if e == nil {
					_ = server.Close()
				}
			}()
		}
	}()
	_, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	command := &cobra.Command{}
	command.SetContext(context.Background())
	keys, e := scanHostKeys(command, "127.0.0.1", port)
	if e != nil || len(keys) != 1 || keys[0].Fingerprint != cryptossh.FingerprintSHA256(signer.PublicKey()) {
		t.Fatal("inspection did not return the peer fingerprint", e)
	}
	app := &App{dataDir: t.TempDir()}
	if _, e = app.trustServerHost(command, "127.0.0.1", port, "SHA256:wrong"); e == nil {
		t.Fatal("unreviewed host key trusted")
	}
	path, e := app.trustServerHost(command, "127.0.0.1", port, keys[0].Fingerprint)
	if e != nil {
		t.Fatal(e)
	}
	data, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	_, _, key, _, _, e := cryptossh.ParseKnownHosts(data)
	if e != nil || cryptossh.FingerprintSHA256(key) != keys[0].Fingerprint {
		t.Fatal("saved key differs from reviewed key")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatal("host trust file is not private")
	}
	if _, e = scanHostKeys(command, "-oProxyCommand=invalid", port); e == nil {
		t.Fatal("SSH option accepted as a host")
	}
}

func TestSetSSHRetainsPinnedHostAndSavesOnlyWorkingCredentials(t *testing.T) {
	app := newTestAppWithLinkedUser(t)
	server, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatal(err)
	}
	server.SSHKnownHostsFile = "fixture-known-hosts"
	server.SSHStrictHostKey = true
	server.SSHPort = 22
	if err := app.store.UpdateServer(app.ctx, server); err != nil {
		t.Fatal(err)
	}
	probes := 0
	app.remoteExecHook = func(cfg ssh.Config, _ time.Duration, _ string) (ssh.Result, error) {
		probes++
		if cfg.KnownHostsFile != server.SSHKnownHostsFile || !cfg.StrictHostKey {
			t.Fatal("lost the reviewed host key")
		}
		if cfg.IdentityFile != "fixture-working-key" {
			return ssh.Result{}, errors.New("fixture rejected SSH identity")
		}
		return ssh.Result{}, nil
	}
	run := func(options ...string) error {
		cmd := app.newServerSetSSHCmd()
		cmd.SetArgs(append([]string{server.Name}, options...))
		return cmd.ExecuteContext(app.ctx)
	}
	if err := run("--ssh-identity", "fixture-wrong-key"); err == nil {
		t.Fatal("invalid identity was accepted")
	}
	saved, err := app.store.GetServerByID(app.ctx, server.ID)
	if err != nil || saved.SSHIdentityFile != server.SSHIdentityFile {
		t.Fatal("failed SSH check changed the node", err)
	}
	if err := run("--ssh-port", "2200"); err == nil || probes != 1 {
		t.Fatal("new port used the previous trust without a reviewed fingerprint", err)
	}
	if err := run("--ssh-identity", "fixture-working-key", "--ssh-user", "operator"); err != nil {
		t.Fatal(err)
	}
	saved, err = app.store.GetServerByID(app.ctx, server.ID)
	if err != nil || saved.SSHUser != "operator" || saved.SSHIdentityFile != "fixture-working-key" ||
		saved.SSHKnownHostsFile != server.SSHKnownHostsFile || !saved.SSHStrictHostKey {
		t.Fatal("known-good credentials were not saved with existing trust", err)
	}
}
