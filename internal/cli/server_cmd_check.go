package cli

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ovpn/internal/model"
	"ovpn/internal/observation"

	"github.com/spf13/cobra"
	cryptossh "golang.org/x/crypto/ssh"
)

type hostKey struct {
	Fingerprint string `json:"fingerprint"`
	Line        string `json:"line"`
}

func scanHostKeys(cmd *cobra.Command, host string, port int) ([]hostKey, error) {
	if strings.TrimSpace(host) != host || host == "" || strings.HasPrefix(host, "-") || strings.ContainsAny(host, " \t\r\n/") || port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid SSH address")
	}
	if net.ParseIP(host) == nil {
		for _, r := range host {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '.' && r != '-' {
				return nil, fmt.Errorf("invalid SSH address")
			}
		}
	}
	out, err := exec.CommandContext(cmd.Context(), "ssh-keyscan", "-T", "5", "-p", strconv.Itoa(port), host).Output()
	if err != nil {
		return nil, fmt.Errorf("SSH_HOST_UNREACHABLE")
	}
	keys := []hostKey{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		_, _, key, _, _, err := cryptossh.ParseKnownHosts([]byte(line))
		if err != nil {
			return nil, fmt.Errorf("SSH_HOST_UNREACHABLE")
		}
		keys = append(keys, hostKey{Fingerprint: cryptossh.FingerprintSHA256(key), Line: line})
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("SSH_HOST_UNREACHABLE")
	}
	return keys, nil
}
func (a *App) newServerInspectCmd() *cobra.Command {
	var host string
	var port int
	cmd := &cobra.Command{Use: "inspect", Short: "Show SSH host fingerprints before trusting a new server", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		keys, err := scanHostKeys(cmd, host, port)
		if err != nil {
			return err
		}
		a.sharedData, _ = json.Marshal(keys)
		for _, key := range keys {
			fmt.Fprintln(a.output(), key.Fingerprint)
		}
		return nil
	}}
	cmd.Flags().StringVar(&host, "host", "", "Server address")
	cmd.Flags().IntVar(&port, "ssh-port", 22, "SSH port")
	_ = cmd.MarkFlagRequired("host")
	return cmd
}
func (a *App) trustServerHost(cmd *cobra.Command, host string, port int, expected string) (string, error) {
	keys, err := scanHostKeys(cmd, host, port)
	if err != nil {
		return "", err
	}
	if err = observation.PrivateDir(a.dataDir); err != nil {
		return "", err
	}
	for _, key := range keys {
		if key.Fingerprint == expected {
			// Pin only the reviewed key, rather than trusting every key returned by the peer.
			path := filepath.Join(a.dataDir, "known_hosts")
			f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
			if err != nil {
				return "", err
			}
			_, err = fmt.Fprintln(f, key.Line)
			e := f.Close()
			if err == nil {
				err = e
			}
			return path, err
		}
	}
	return "", fmt.Errorf("SSH_HOST_KEY_CHANGED")
}
func (a *App) newServerTestCmd() *cobra.Command {
	return &cobra.Command{Use: "test <server>", Short: "Check SSH access and server requirements", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		server, err := a.store.GetServerByName(a.ctx, args[0])
		if err != nil {
			return err
		}
		if err := a.verifySSHAccess(*server); err != nil {
			return err
		}
		fmt.Fprintln(a.output(), "SSH access verified. The server is ready for installation.")
		return nil
	}}
}

// A verified candidate replaces the saved SSH settings only after it works.
func (a *App) newServerSetSSHCmd() *cobra.Command {
	var user, identity, fingerprint string
	var port int
	cmd := &cobra.Command{Use: "set-ssh <server>", Short: "Update and verify SSH access", Args: cobra.ExactArgs(1)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		server, err := a.store.GetServerByName(a.ctx, args[0])
		if err != nil {
			return err
		}
		candidate := *server
		if cmd.Flags().Changed("ssh-user") {
			if strings.TrimSpace(user) == "" {
				return fmt.Errorf("SSH user is required")
			}
			candidate.SSHUser = strings.TrimSpace(user)
		}
		if cmd.Flags().Changed("ssh-identity") {
			candidate.SSHIdentityFile = identity
		}
		if cmd.Flags().Changed("ssh-port") {
			if port < 1 || port > 65535 {
				return fmt.Errorf("invalid SSH port")
			}
			candidate.SSHPort = port
		}
		if candidate.SSHPort != server.SSHPort && fingerprint == "" {
			return fmt.Errorf("SSH_HOST_KEY_CHANGED")
		}
		if a.dryRun {
			fmt.Fprintln(a.output(), "SSH settings unchanged in dry-run mode.")
			return nil
		}
		if fingerprint != "" {
			candidate.SSHKnownHostsFile, err = a.trustServerHost(cmd, candidate.Host, candidate.SSHPort, fingerprint)
			if err != nil {
				return err
			}
			candidate.SSHStrictHostKey = true
		}
		if err := a.verifySSHAccess(candidate); err != nil {
			return err
		}
		if err := a.store.UpdateServer(a.ctx, &candidate); err != nil {
			return err
		}
		fmt.Fprintln(a.output(), "SSH access verified and settings saved.")
		return nil
	}
	cmd.Flags().StringVar(&user, "ssh-user", "", "SSH user")
	cmd.Flags().IntVar(&port, "ssh-port", 0, "SSH port")
	cmd.Flags().StringVar(&identity, "ssh-identity", "", "SSH identity path, or empty to use the agent")
	cmd.Flags().StringVar(&fingerprint, "trust-fingerprint", "", "Reviewed SHA256 host fingerprint for a new SSH port")
	return cmd
}

func (a *App) verifySSHAccess(server model.Server) error {
	_, err := a.execRemote(a.newRunner("server.test"), sshFromServer(server), 20*time.Second, "set -e; command -v sh >/dev/null; sudo -n true; test -r /etc/os-release; uname -m")
	if err != nil {
		return fmt.Errorf("SSH_ACCESS_FAILED")
	}
	return nil
}
