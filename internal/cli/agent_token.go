package cli

import (
	cryptorand "crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"ovpn/internal/util"
)

// agentTokenFileName is the local file under the data dir that persists the generated
// ovpn-agent bearer token so it stays stable across deploys.
const agentTokenFileName = "agent-token"

// agentToken resolves the bearer token used to authenticate CLI -> ovpn-agent mutating calls.
//
// Precedence: an explicit OVPN_AGENT_TOKEN env (operator-managed), then a token persisted under
// the local data dir, otherwise a freshly generated token that is persisted for reuse. The token
// is rendered into the remote .env so the agent container and the operator's curl calls always
// share the same value without storing it anywhere except the host and the operator machine.
func (a *App) agentToken() (string, error) {
	if v := strings.TrimSpace(os.Getenv("OVPN_AGENT_TOKEN")); v != "" {
		return v, nil
	}
	dataDir := a.dataDir
	if strings.TrimSpace(dataDir) == "" {
		dataDir = util.DefaultDataDir()
	}
	secretsDir := filepath.Join(dataDir, "secrets")
	tokenPath := filepath.Join(secretsDir, agentTokenFileName)
	b, err := os.ReadFile(tokenPath) // #nosec G304 -- fixed operator-local path under the data dir.
	switch {
	case err == nil:
		if v := strings.TrimSpace(string(b)); v != "" {
			return v, nil
		}
	case !errors.Is(err, os.ErrNotExist):
		return "", fmt.Errorf("read agent token %s: %w", tokenPath, err)
	}

	token, err := newRandomToken(32)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		return "", fmt.Errorf("create secrets dir %s: %w", secretsDir, err)
	}
	if err := os.WriteFile(tokenPath, []byte(token+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("persist agent token %s: %w", tokenPath, err)
	}
	a.log().Info("generated ovpn-agent auth token", "path", tokenPath)
	return token, nil
}

// newRandomToken returns a URL-safe random token carrying n bytes of entropy.
func newRandomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := cryptorand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
