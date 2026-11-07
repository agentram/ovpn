package probe

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"sync/atomic"
	"time"
)

type DialContext func(context.Context, string, string) (net.Conn, error)

type Options struct {
	URL            string
	ExpectedStatus int
	Timeout        time.Duration
	MaxBodyBytes   int64
	RootCAs        *x509.CertPool
	ServerName     string
}

// Result deliberately excludes URLs, response bodies and client credentials.
type Result struct {
	Success    bool   `json:"success"`
	StatusCode int    `json:"status_code,omitempty"`
	DurationMS int64  `json:"duration_ms"`
	TTFBMS     int64  `json:"ttfb_ms"`
	Bytes      int64  `json:"bytes"`
	ErrorCode  string `json:"error_code,omitempty"`
}

func (o Options) Validate() error {
	u, err := url.Parse(o.URL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return fmt.Errorf("--url must be an absolute HTTPS URL without credentials or a fragment")
	}
	if o.ExpectedStatus < 100 || o.ExpectedStatus > 599 {
		return fmt.Errorf("--expect-status must be between 100 and 599")
	}
	if o.Timeout <= 0 || o.Timeout > 5*time.Minute {
		return fmt.Errorf("--timeout must be greater than zero and at most 5m")
	}
	if o.MaxBodyBytes <= 0 || o.MaxBodyBytes > 16<<20 {
		return fmt.Errorf("--max-bytes must be between 1 and 16777216")
	}
	return nil
}

// Check sends exactly one HTTPS request using only the supplied tunnel dialer.
func Check(ctx context.Context, opts Options, dial DialContext) (result Result, err error) {
	if err := opts.Validate(); err != nil {
		return result, err
	}
	if dial == nil {
		return result, fmt.Errorf("a tunnel dialer is required")
	}
	start := time.Now()
	defer func() { result.DurationMS = time.Since(start).Milliseconds() }()
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	var firstByte atomic.Int64
	trace := &httptrace.ClientTrace{GotFirstResponseByte: func() { firstByte.Store(time.Since(start).Nanoseconds()) }}
	request, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), http.MethodGet, opts.URL, nil)
	if err != nil {
		return result, fmt.Errorf("invalid HTTPS request")
	}
	transport := &http.Transport{
		DialContext: dial, DisableKeepAlives: true, DisableCompression: true,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: opts.RootCAs, ServerName: opts.ServerName},
		TLSHandshakeTimeout: opts.Timeout, ResponseHeaderTimeout: opts.Timeout,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	response, requestErr := client.Do(request)
	if requestErr != nil {
		result.ErrorCode = "request_failed"
		var timeout net.Error
		var certificate *tls.CertificateVerificationError
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.As(requestErr, &timeout) && timeout.Timeout():
			result.ErrorCode = "timeout"
		case errors.Is(ctx.Err(), context.Canceled):
			result.ErrorCode = "canceled"
		case errors.As(requestErr, &certificate):
			result.ErrorCode = "tls_verification_failed"
		}
		return result, fmt.Errorf("traffic probe failed (%s)", result.ErrorCode)
	}
	defer response.Body.Close()
	result.StatusCode = response.StatusCode
	result.TTFBMS = time.Duration(firstByte.Load()).Milliseconds()
	if response.StatusCode != opts.ExpectedStatus {
		result.ErrorCode = "unexpected_http_status"
		return result, fmt.Errorf("traffic probe received HTTP %d, expected %d", response.StatusCode, opts.ExpectedStatus)
	}
	result.Bytes, err = io.Copy(io.Discard, io.LimitReader(response.Body, opts.MaxBodyBytes+1))
	if err != nil {
		result.ErrorCode = "response_read_failed"
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			result.ErrorCode = "timeout"
		} else if errors.Is(ctx.Err(), context.Canceled) {
			result.ErrorCode = "canceled"
		}
		return result, fmt.Errorf("traffic probe failed (%s)", result.ErrorCode)
	}
	if result.Bytes > opts.MaxBodyBytes {
		result.ErrorCode = "body_limit_exceeded"
		return result, fmt.Errorf("traffic probe response exceeds --max-bytes")
	}
	result.Success = true
	return result, nil
}
