package subscription

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

type Handler struct {
	store *Store
	now   func() time.Time
}

func NewHandler(dir string) (*Handler, error) {
	store, err := Open(dir)
	if err != nil {
		return nil, err
	}
	return &Handler{store: store, now: time.Now}, nil
}

func (h *Handler) Close() error { return h.store.Close() }

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path == "/health" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if r.Method != http.MethodHead {
			_, _ = io.WriteString(w, "ok\n")
		}
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/sub/"), "/")
	if !strings.HasPrefix(r.URL.Path, "/sub/") || len(parts) != 2 || !validID(parts[0]) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	token, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(token) != 32 || base64.RawURLEncoding.EncodeToString(token) != parts[1] {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	bundle, err := h.store.Read(parts[0])
	if err != nil || subtle.ConstantTimeCompare([]byte(bundle.TokenHash), []byte(tokenHash(parts[1]))) != 1 {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	data, err := Encode(bundle.Entries, h.now(), "base64")
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Profile-Title", "base64:"+base64.StdEncoding.EncodeToString([]byte(bundle.Title)))
	w.Header().Set("Profile-Update-Interval", "6")
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}

type ServeOptions struct {
	Dir, Listen, CertFile, KeyFile string
}

func (o ServeOptions) Validate() error {
	if o.Dir == "" {
		return fmt.Errorf("--bundle-dir is required")
	}
	host, _, err := net.SplitHostPort(o.Listen)
	if err != nil {
		return fmt.Errorf("--listen must be a host:port address")
	}
	if (o.CertFile == "") != (o.KeyFile == "") {
		return fmt.Errorf("--tls-cert and --tls-key must be supplied together")
	}
	ip, err := netip.ParseAddr(host)
	if o.CertFile == "" && (err != nil || !ip.IsLoopback()) {
		return fmt.Errorf("TLS is required when listening outside a literal loopback address")
	}
	return nil
}

// Serve reads prepared bundles only. It never opens the control database or uses SSH.
func Serve(ctx context.Context, opts ServeOptions) error {
	if err := opts.Validate(); err != nil {
		return err
	}
	handler, err := NewHandler(opts.Dir)
	if err != nil {
		return err
	}
	defer handler.Close()
	server := &http.Server{
		Handler: handler, ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second,
		IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192,
		ErrorLog: log.New(io.Discard, "", 0),
	}
	if opts.CertFile != "" {
		cert, err := tls.LoadX509KeyPair(opts.CertFile, opts.KeyFile)
		if err != nil {
			return fmt.Errorf("load subscription TLS certificate: %w", err)
		}
		server.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}}
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", opts.Listen)
	if err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() {
		if server.TLSConfig != nil {
			done <- server.ServeTLS(listener, "", "")
		} else {
			done <- server.Serve(listener)
		}
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
			return err
		}
		err := <-done
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
