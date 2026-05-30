package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"ovpn/internal/telegrambot"
)

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

// requireAgentToken guards mutating endpoints with a bearer token when one is configured.
//
// The agent binds to 127.0.0.1 on the host but is reachable by every container that shares its
// Docker network (Prometheus, Grafana, cAdvisor, the Telegram bot). Without a token, any of those
// images could call the mutating endpoints (/runtime/user/add, /quota/reset, ...) and take over
// VPN user state. Read-only methods stay open so Prometheus scraping and health checks keep
// working; when no token is set the agent behaves exactly as before (backward compatible).
func requireAgentToken(token string, next http.Handler) http.Handler {
	token = strings.TrimSpace(token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token != "" && !isReadOnlyMethod(r.Method) && !bearerTokenMatches(r.Header.Get("Authorization"), token) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isReadOnlyMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
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
