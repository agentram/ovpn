package subscription

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
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
	store   *Store
	invites *Invites
	now     func() time.Time
}

func NewHandler(dir string) (*Handler, error) {
	store, err := Open(dir)
	if err != nil {
		return nil, err
	}
	return &Handler{store: store, now: time.Now}, nil
}

func (h *Handler) Close() error {
	if h.invites != nil {
		return errors.Join(h.invites.Close(), h.store.Close())
	}
	return h.store.Close()
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.URL.Path == "/activate" && h.invites != nil {
		h.activate(w, r)
		return
	}
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
	if strings.HasPrefix(r.URL.Path, "/sub/") {
		w.Header().Set(deliveryHeader, deliveryVersion)
	}
	if h.invites != nil && strings.HasPrefix(r.URL.Path, "/device/") {
		w.Header().Set(deviceHeader, deliveryVersion)
	}
	bundle, err := h.lookup(r.URL.Path)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if bundle.ManagedKey != "" {
		if bundle.Disabled {
			w.Header().Set("OVPN-Subscription-State", "disabled")
			w.WriteHeader(http.StatusGone)
			return
		}
		// IDs and revisions are for correlating a report with the publisher.
		// Neither value grants access to any delivery endpoint.
		w.Header().Set("OVPN-Subscription-ID", bundle.ID)
	}
	data, err := Encode(bundle.Entries, h.now(), "base64")
	if err != nil {
		if bundle.ManagedKey != "" {
			w.Header().Set("OVPN-Subscription-State", "empty")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if bundle.ManagedKey != "" {
		w.Header().Set("OVPN-Subscription-Revision", ProfileRevision(string(data)))
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Profile-Title", "base64:"+base64.StdEncoding.EncodeToString([]byte(bundle.Title)))
	w.Header().Set("Profile-Update-Interval", "6")
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}

func (h *Handler) lookup(path string) (Bundle, error) {
	parts := strings.Split(path, "/")
	if len(parts) != 4 || parts[0] != "" {
		return Bundle{}, ErrNotFound
	}
	switch parts[1] {
	case "device":
		if h.invites != nil {
			return h.invites.deviceBundle(h.store, parts[2], parts[3])
		}
	case "sub":
		if !validToken(parts[3]) {
			return Bundle{}, ErrNotFound
		}
		bundle, err := h.store.Read(parts[2])
		if err == nil && subtle.ConstantTimeCompare([]byte(bundle.TokenHash), []byte(tokenHash(parts[3]))) == 1 {
			return bundle, nil
		}
	}
	return Bundle{}, ErrNotFound
}

const (
	deliveryHeader   = "OVPN-Delivery-Version"
	deviceHeader     = "OVPN-Device-Version"
	activationHeader = "OVPN-Activation-Version"
	deliveryVersion  = "1"
)

func (h *Handler) activate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set(activationHeader, deliveryVersion)
	var request struct {
		Code    string `json:"code"`
		Session string `json:"session"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512))
	if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	status := http.StatusNoContent
	err := h.invites.redeem(h.store, request.Code, request.Session, h.now())
	switch {
	case errors.Is(err, ErrNotFound):
		status = http.StatusNotFound
	case err != nil:
		status = http.StatusServiceUnavailable
	}
	w.WriteHeader(status)
}

type ServeOptions struct {
	Dir, InviteDir, Listen, CertFile, KeyFile string
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
	if opts.InviteDir != "" {
		handler.invites, err = OpenInvites(opts.InviteDir)
		if err != nil {
			return err
		}
	}
	server := &http.Server{
		Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second,
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
