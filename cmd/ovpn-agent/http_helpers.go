package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ovpn/internal/telegrambot"
)

// agentTokenFile is where the agent persists its bearer token inside the data directory so auth
// survives a redeploy whose rendered env happens to carry no token.
const agentTokenFile = "agent.token"

// resolveAgentToken determines the effective bearer token and makes auth "sticky" for this agent.
//
// An explicit OVPN_AGENT_TOKEN always wins and is persisted. When this agent starts with an empty
// token (its rendered env lost the value), the previously persisted token is reused so a
// token-aware agent does not silently fall back to unauthenticated. To intentionally disable auth,
// clear the token and remove the persisted file under the data directory.
//
// Scope: this only helps when a token-aware agent binary is running. A deploy performed by a
// pre-auth ovpn replaces the agent binary itself with an unauthenticated build, which never reads
// this file; guarding against that requires keeping every operator on a token-aware ovpn release.
func resolveAgentToken(envToken, dbPath string, logger *slog.Logger) string {
	// tokenPath is a fixed filename under the operator-provided data dir (a daemon flag), not
	// network input, so the path-traversal warnings on these file ops are not applicable.
	tokenPath := filepath.Join(dbPath, agentTokenFile)
	if envToken != "" {
		if err := os.MkdirAll(dbPath, 0o700); err != nil {
			logger.Warn("create data dir for agent token failed", "path", dbPath, "error", err)
		} else if err := os.WriteFile(tokenPath, []byte(envToken), 0o600); err != nil { // #nosec G304 G703 -- operator-controlled data dir, fixed filename.
			logger.Warn("persist agent token failed", "path", tokenPath, "error", err)
		}
		return envToken
	}
	b, err := os.ReadFile(tokenPath) // #nosec G304 G703 -- operator-controlled data dir, fixed filename.
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			logger.Warn("read persisted agent token failed", "path", tokenPath, "error", err)
		}
		return ""
	}
	persisted := strings.TrimSpace(string(b))
	if persisted != "" {
		logger.Warn("OVPN_AGENT_TOKEN is empty; reusing persisted token so agent auth stays enabled",
			"path", tokenPath,
			"hint", "the rendered env carried no token; set OVPN_AGENT_TOKEN, or remove this file to disable auth")
	}
	return persisted
}

// envInt64 reads key as a base-10 int64, returning fallback when it is unset or unparseable.
func envInt64(key string, fallback int64) int64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fallback
	}
	return v
}

func envOr(key string, fallback string) string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	return raw
}

// handleQuotaPolicies serves GET /quota/policies, returning the stored quota policies as JSON.
func handleQuotaPolicies(store quotaPolicyLister, logger *slog.Logger, metrics *agentMetrics) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		policies, err := store.ListQuotaPolicies(r.Context())
		if err != nil {
			logger.Warn("list quota policies failed", "error", err)
			if metrics != nil {
				metrics.OnDBWriteError("list_quota_policies")
			}
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, policies)
	}
}

func isRuntimeUserAbsentError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(text, "not found") ||
		strings.Contains(text, "not exist") ||
		strings.Contains(text, "failed to remove") && strings.Contains(text, "user")
}

func isRuntimeUserAlreadyPresentError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(text, "already exists") ||
		strings.Contains(text, "already exist")
}

// postNotifyEvent delivers a NotifyEvent to the local Telegram bot notify endpoint with a short timeout.
func postNotifyEvent(ctx context.Context, payload telegrambot.NotifyEvent) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, telegramNotifyEndpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("notify endpoint returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// writeJSON encodes payload as a JSON response body with the given status code.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

// WriteHeader records the response status code before delegating to the wrapped ResponseWriter.
func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// readOnlyAgentPaths are the endpoints safe to serve without the bearer token: Prometheus
// scraping, health checks, and read-only stats/status queries. This allowlist drives a
// default-deny policy, so any other path — including a side-effecting endpoint like /collect that
// accepts non-POST methods — requires the token, and a newly added endpoint is protected by default.
var readOnlyAgentPaths = map[string]bool{
	"/metrics":        true,
	"/health":         true,
	"/stats/total":    true,
	"/stats/daily":    true,
	"/quota/status":   true,
	"/users/status":   true,
	"/quota/policies": true,
}

// requireAgentToken guards every non-read-only endpoint with a bearer token when one is configured.
//
// The agent binds to 127.0.0.1 on the host but is reachable by every container that shares its
// Docker network (Prometheus, Grafana, cAdvisor, the Telegram bot). Without a token, any of those
// images could call the mutating endpoints (/runtime/user/add, /quota/reset, /collect, ...) and
// take over VPN user state. Matching on path rather than HTTP method ensures a side-effecting GET
// such as /collect cannot slip through. When no token is set the agent behaves as before.
func requireAgentToken(token string, next http.Handler) http.Handler {
	token = strings.TrimSpace(token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token != "" && !readOnlyAgentPaths[r.URL.Path] && !bearerTokenMatches(r.Header.Get("Authorization"), token) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func bearerTokenMatches(header, token string) bool {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	got := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	return subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}

func withRequestLogging(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(rec, r)
		attrs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
		}
		switch {
		case rec.status >= 500:
			logger.Error("http request completed", attrs...)
		case rec.status >= 400:
			logger.Warn("http request completed", attrs...)
		default:
			logger.Debug("http request completed", attrs...)
		}
	})
}
