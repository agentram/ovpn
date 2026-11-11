package subscription

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// VerifyDelivery checks TLS, routing, and the published snapshot before a setup
// code is issued. Credentials remain in the worker and redirects are rejected.
func VerifyDelivery(ctx context.Context, origin, link string, expected []byte) error {
	if err := ValidateBaseURL(origin); err != nil {
		return fmt.Errorf("DELIVERY_UNAVAILABLE")
	}
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return verifyDelivery(ctx, origin, link, expected, client)
}
func verifyDelivery(ctx context.Context, origin, link string, expected []byte, client *http.Client) error {
	for _, target := range []struct {
		url  string
		body string
	}{{strings.TrimSuffix(origin, "/") + "/health", "ok"}, {link, strings.TrimSpace(string(expected))}} {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.url, nil)
		if err != nil {
			return fmt.Errorf("DELIVERY_UNAVAILABLE")
		}
		response, err := client.Do(request)
		if err != nil {
			return fmt.Errorf("DELIVERY_UNAVAILABLE")
		}
		body, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
		_ = response.Body.Close()
		if err != nil || response.StatusCode != http.StatusOK || len(body) > (1<<20) || strings.TrimSpace(string(body)) != target.body {
			return fmt.Errorf("DELIVERY_UNAVAILABLE")
		}
	}
	return nil
}

func VerifyDeliveryHealth(ctx context.Context, origin string) error {
	return VerifyDelivery(ctx, origin, strings.TrimSuffix(origin, "/")+"/health", []byte("ok"))
}
