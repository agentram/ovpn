package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"ovpn/internal/defaults"
	"ovpn/internal/model"
)

const (
	vlessEncryptionAuthHeader = "Authentication: ML-KEM-768, Post-Quantum"
)

func parseVLESSEncryptionOutput(raw []byte) (string, string, error) {
	// The strict field/section contract below was verified with Xray 26.7.28.
	// Reject format changes before any generated key material is persisted.
	var clientEncryption string
	var serverDecryption string
	inMLKEMSection := false
	foundMLKEMSection := false
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	// Xray may add explanatory text around the generated values. Keep a bounded
	// buffer so a long non-secret line does not reject an otherwise valid pair.
	scanner.Buffer(make([]byte, 4*1024), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "Authentication:") {
			inMLKEMSection = line == vlessEncryptionAuthHeader
			foundMLKEMSection = foundMLKEMSection || inMLKEMSection
			continue
		}
		if !inMLKEMSection {
			continue
		}
		if strings.HasPrefix(line, `"`) &&
			!strings.HasPrefix(line, `"encryption"`) &&
			!strings.HasPrefix(line, `"decryption"`) {
			return "", "", errors.New("parse ML-KEM VLESS Encryption output: unexpected field")
		}
		if !strings.HasPrefix(line, `"encryption"`) && !strings.HasPrefix(line, `"decryption"`) {
			continue
		}
		var item map[string]string
		if err := json.Unmarshal([]byte("{"+line+"}"), &item); err != nil {
			return "", "", fmt.Errorf("parse ML-KEM VLESS Encryption output: %w", err)
		}
		if len(item) != 1 {
			return "", "", errors.New("parse ML-KEM VLESS Encryption output: expected exactly one field per line")
		}
		if value, ok := item["encryption"]; ok {
			if clientEncryption != "" {
				return "", "", errors.New("parse ML-KEM VLESS Encryption output: duplicate encryption value")
			}
			clientEncryption = strings.TrimSpace(value)
		}
		if value, ok := item["decryption"]; ok {
			if serverDecryption != "" {
				return "", "", errors.New("parse ML-KEM VLESS Encryption output: duplicate decryption value")
			}
			serverDecryption = strings.TrimSpace(value)
		}
	}
	if err := scanner.Err(); err != nil {
		return "", "", fmt.Errorf("scan VLESS Encryption output: %w", err)
	}
	if !foundMLKEMSection {
		return "", "", errors.New("xray vlessenc did not return an ML-KEM-768 section")
	}
	if !model.IsValidVLESSEncryptionClientValue(clientEncryption) ||
		!model.IsValidVLESSEncryptionServerValue(serverDecryption) {
		return "", "", errors.New("xray vlessenc did not return the expected ML-KEM-768 native 0-RTT/600s pair")
	}
	return clientEncryption, serverDecryption, nil
}

func (a *App) generateVLESSEncryptionPair(target model.Server) (string, string, error) {
	image := defaults.DefaultXrayImage(target.XrayVersion)
	command := strings.Join([]string{
		"set -eu",
		"image=" + shellQuote(image),
		`if ! sudo -n docker image inspect "$image" >/dev/null 2>&1; then sudo -n docker pull "$image" >/dev/null; fi`,
		`sudo -n docker run --rm "$image" vlessenc`,
	}, "; ")

	runner := a.newRunner("server.profile.vlessenc")
	result, err := a.execRemote(runner, sshFromServer(target), 2*time.Minute, command)
	if err != nil {
		return "", "", fmt.Errorf("generate VLESS Encryption pair on server %s with %s: Docker is unavailable or the pinned Xray image could not be pulled on the target host: %w", target.Name, image, err)
	}
	clientEncryption, serverDecryption, err := parseVLESSEncryptionOutput([]byte(result.Stdout))
	if err != nil {
		return "", "", fmt.Errorf("generate VLESS Encryption pair on server %s with %s: %w", target.Name, image, err)
	}
	return clientEncryption, serverDecryption, nil
}

func (a *App) ensureVLESSEncryptionForServer(target *model.Server) error {
	if target == nil {
		return errors.New("server is required")
	}
	if !target.IsVPN() {
		return fmt.Errorf("%s is only supported on vpn servers", model.TransportProfileVLESSEncXHTTP)
	}
	servers, err := a.store.ListServers(a.ctx)
	if err != nil {
		return err
	}
	vpnServers := make([]model.Server, 0, len(servers))
	for _, srv := range servers {
		if srv.IsVPN() {
			vpnServers = append(vpnServers, srv)
		}
	}
	if len(vpnServers) == 0 {
		return errors.New("no vpn servers found")
	}
	sort.Slice(vpnServers, func(i, j int) bool { return vpnServers[i].Name < vpnServers[j].Name })

	var clientEncryption string
	var serverDecryption string
	for _, srv := range vpnServers {
		client := strings.TrimSpace(srv.VLESSClientEncryption)
		server := strings.TrimSpace(srv.VLESSServerDecryption)
		if (client == "") != (server == "") {
			return fmt.Errorf("VLESS Encryption key pair is incomplete on server %s", srv.Name)
		}
		if client == "" {
			continue
		}
		if clientEncryption == "" {
			clientEncryption, serverDecryption = client, server
			continue
		}
		if client != clientEncryption || server != serverDecryption {
			return fmt.Errorf("VLESS Encryption cluster parity check failed: server %s uses a different key pair", srv.Name)
		}
	}
	if clientEncryption == "" {
		clientEncryption, serverDecryption, err = a.generateVLESSEncryptionPair(*target)
		if err != nil {
			return err
		}
	}
	ids := make([]int64, 0, len(vpnServers))
	for _, srv := range vpnServers {
		ids = append(ids, srv.ID)
	}
	if err := a.store.SetVLESSEncryptionForServers(a.ctx, ids, clientEncryption, serverDecryption); err != nil {
		return fmt.Errorf("store cluster VLESS Encryption key pair: %w", err)
	}
	target.VLESSClientEncryption = clientEncryption
	target.VLESSServerDecryption = serverDecryption
	return nil
}

func ensureVLESSEncryptionParityForServers(servers []model.Server) error {
	var baseline *model.Server
	for i := range servers {
		srv := &servers[i]
		client := strings.TrimSpace(srv.VLESSClientEncryption)
		server := strings.TrimSpace(srv.VLESSServerDecryption)
		if (client == "") != (server == "") {
			return fmt.Errorf("VLESS Encryption key pair is incomplete on server %s", srv.Name)
		}
		if client == "" {
			continue
		}
		if baseline == nil {
			baseline = srv
			continue
		}
		if client != strings.TrimSpace(baseline.VLESSClientEncryption) ||
			server != strings.TrimSpace(baseline.VLESSServerDecryption) {
			return fmt.Errorf("VLESS Encryption parity check failed: server %s differs from %s", srv.Name, baseline.Name)
		}
	}
	return nil
}

func (a *App) ensureVLESSEncryptionParity() error {
	servers, err := a.store.ListServers(a.ctx)
	if err != nil {
		return err
	}
	vpnServers := make([]model.Server, 0, len(servers))
	for _, srv := range servers {
		if srv.IsVPN() {
			vpnServers = append(vpnServers, srv)
		}
	}
	return ensureVLESSEncryptionParityForServers(vpnServers)
}
