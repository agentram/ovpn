package cli

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
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
