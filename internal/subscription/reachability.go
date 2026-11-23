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
	client := deliveryClient()
	defer client.CloseIdleConnections()
	return verifyDelivery(ctx, origin, link, expected, client)
}

func deliveryClient() *http.Client {
	return &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 8 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
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

// VerifySubscriptionRoute checks the HTTPS subscription handler before publishing.
func VerifySubscriptionRoute(ctx context.Context, origin string) error {
	if !strings.HasPrefix(strings.ToLower(origin), "https://") || ValidateBaseURL(origin) != nil {
		return fmt.Errorf("DELIVERY_UNAVAILABLE")
	}
	client := deliveryClient()
	defer client.CloseIdleConnections()
	target := strings.TrimSuffix(origin, "/") + "/sub/probe/invalid"
	return verifyRoute(ctx, client, http.MethodGet, target, deliveryHeader, nil)
}

// VerifyActivation checks the actual HTTPS POST route with an invalid code.
// The probe exercises routing and handler configuration without claiming a code.
func VerifyActivation(ctx context.Context, origin string) error {
	if !strings.HasPrefix(strings.ToLower(origin), "https://") || ValidateBaseURL(origin) != nil {
		return fmt.Errorf("DELIVERY_UNAVAILABLE")
	}
	client := deliveryClient()
	defer client.CloseIdleConnections()
	return verifyActivation(ctx, origin, client)
}

func verifyActivation(ctx context.Context, origin string, client *http.Client) error {
	target := strings.TrimSuffix(origin, "/") + "/activate"
	if err := verifyRoute(ctx, client, http.MethodPost, target, activationHeader,
		strings.NewReader(`{"code":"","session":""}`)); err != nil {
		return err
	}
	device := strings.TrimSuffix(origin, "/") + "/device/probe/invalid"
	return verifyRoute(ctx, client, http.MethodGet, device, deviceHeader, nil)
}

func verifyRoute(ctx context.Context, client *http.Client, method, target, header string, body io.Reader) error {
	request, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return fmt.Errorf("DELIVERY_UNAVAILABLE")
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("DELIVERY_UNAVAILABLE")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound || response.Header.Get(header) != deliveryVersion {
		return fmt.Errorf("DELIVERY_UNAVAILABLE")
	}
	return nil
}
