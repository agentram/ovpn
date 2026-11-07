package probe

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHTTPSChecksStatusLimitsAndRedirects(t *testing.T) {
	var redirected atomic.Int64
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			fmt.Fprint(w, "fixture")
		case "/large":
			fmt.Fprint(w, strings.Repeat("x", 100))
		case "/redirect":
			http.Redirect(w, r, "/destination", http.StatusFound)
		case "/destination":
			redirected.Add(1)
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer srv.Close()
	roots := x509.NewCertPool()
	roots.AddCert(srv.Certificate())
	for _, tc := range []struct {
		path, code string
		limit      int64
	}{
		{"/ok", "", 50}, {"/large", "body_limit_exceeded", 20},
		{"/redirect", "unexpected_http_status", 50}, {"/unavailable", "unexpected_http_status", 50},
	} {
		t.Run(tc.path, func(t *testing.T) {
			var calls int
			dial := func(ctx context.Context, network, address string) (net.Conn, error) {
				calls++
				return (&net.Dialer{}).DialContext(ctx, network, address)
			}
			result, err := Check(context.Background(), Options{URL: srv.URL + tc.path, ExpectedStatus: 200, Timeout: time.Second, MaxBodyBytes: tc.limit, RootCAs: roots}, dial)
			if result.ErrorCode != tc.code || result.Success != (tc.code == "") || (err == nil) != result.Success || calls != 1 {
				t.Fatalf("unexpected probe: %+v, %v; dial calls %d", result, err, calls)
			}
		})
	}
	if redirected.Load() != 0 {
		t.Fatal("probe followed a redirect")
	}
}

func TestProbeNeverFallsBackToDirectOrEnvironmentProxy(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	var hits atomic.Int64
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer srv.Close()
	opts := Options{URL: srv.URL + "/?token=do-not-echo", ExpectedStatus: 200, Timeout: time.Second, MaxBodyBytes: 50}
	result, err := Check(context.Background(), opts, func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("sensitive-credential-from-tunnel")
	})
	if err == nil || result.Success || hits.Load() != 0 || strings.Contains(err.Error(), "sensitive") || strings.Contains(err.Error(), "do-not-echo") {
		t.Fatalf("unexpected fallback or error disclosure: %+v, %v", result, err)
	}
	if _, err := Check(context.Background(), opts, nil); err == nil {
		t.Fatal("nil dialer must fail")
	}
	roots := x509.NewCertPool()
	roots.AddCert(srv.Certificate())
	opts.URL = "https://probe.example.net/"
	opts.RootCAs = roots
	opts.ServerName = srv.Certificate().DNSNames[0]
	result, err = Check(context.Background(), opts, func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "probe.example.net:443" {
			t.Errorf("request used environment proxy: %s", address)
		}
		return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
	})
	if err != nil || !result.Success || hits.Load() != 1 {
		t.Fatalf("tunnel request failed with environment proxy configured: %+v, %v", result, err)
	}
}

func TestProbeTimeoutIncludesResponseBody(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	roots := x509.NewCertPool()
	roots.AddCert(srv.Certificate())
	result, err := Check(context.Background(), Options{URL: srv.URL, ExpectedStatus: 200, Timeout: 50 * time.Millisecond, MaxBodyBytes: 100, RootCAs: roots}, (&net.Dialer{}).DialContext)
	if err == nil || result.Success || result.StatusCode != 200 || result.ErrorCode != "timeout" {
		t.Fatalf("body read did not respect timeout: %+v, %v", result, err)
	}
}

func TestProbeTimeoutAndTLSVerification(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	opts := Options{URL: srv.URL, ExpectedStatus: 200, Timeout: time.Second, MaxBodyBytes: 50}
	result, err := Check(context.Background(), opts, (&net.Dialer{}).DialContext)
	if err == nil || result.ErrorCode != "tls_verification_failed" {
		t.Fatalf("untrusted certificate accepted: %+v, %v", result, err)
	}
	opts.Timeout = 20 * time.Millisecond
	result, err = Check(context.Background(), opts, func(ctx context.Context, _, _ string) (net.Conn, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if err == nil || result.ErrorCode != "timeout" {
		t.Fatalf("timeout not reported: %+v, %v", result, err)
	}
}

func TestProbeRejectsInvalidOptions(t *testing.T) {
	base := Options{URL: "https://example.net/", ExpectedStatus: 200, Timeout: time.Second, MaxBodyBytes: 50}
	for _, change := range []func(*Options){
		func(o *Options) { o.URL = "http://example.net/" },
		func(o *Options) { o.URL = "https://user:password@example.net/" },
		func(o *Options) { o.ExpectedStatus = 600 },
		func(o *Options) { o.Timeout = 0 },
		func(o *Options) { o.MaxBodyBytes = 0 },
	} {
		opts := base
		change(&opts)
		if opts.Validate() == nil {
			t.Fatal("invalid options accepted")
		}
	}
}
