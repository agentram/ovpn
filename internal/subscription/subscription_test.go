package subscription

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const fixtureLink = "vless://11111111-1111-1111-1111-111111111111@vpn.example.net:443?encryption=none&security=reality&type=tcp&sni=target.example.net&pbk=public-key&sid=aabb&fp=firefox&flow=xtls-rprx-vision#fixture"

func openFixture(t *testing.T) (string, *Store, *Handler) {
	t.Helper()
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	h, err := NewHandler(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })
	return dir, store, h
}

func request(h http.Handler, method, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w
}

func TestStableSubscriptionUpdateRotationAndRevocation(t *testing.T) {
	dir, store, handler := openFixture(t)
	id, token, err := store.Publish("", "alice", "Test subscription", []Entry{{Link: fixtureLink}}, false)
	if err != nil {
		t.Fatal(err)
	}
	path := "/sub/" + id + "/" + token
	w := request(handler, http.MethodGet, path)
	decoded, err := base64.StdEncoding.DecodeString(w.Body.String())
	if w.Code != http.StatusOK || err != nil || string(decoded) != fixtureLink+"\n" {
		t.Fatal("subscription did not deliver the client link")
	}
	if w.Header().Get("Cache-Control") != "no-store, private" || w.Header().Get("Profile-Update-Interval") != "6" {
		t.Fatal("subscription caching or refresh headers are missing")
	}
	file := filepath.Join(dir, id+".json")
	raw, _ := os.ReadFile(file)
	info, _ := os.Stat(file)
	if strings.Contains(string(raw), token) || info.Mode().Perm() != 0o600 {
		t.Fatal("plaintext delivery token persisted or bundle mode is incorrect")
	}
	if _, _, err := store.Publish(id, "bob", "", []Entry{{Link: fixtureLink}}, false); err == nil {
		t.Fatal("another user's subscription was overwritten")
	}
	replacement := strings.ReplaceAll(fixtureLink, "vpn.example.net", "replacement.example.net")
	_, newToken, err := store.Publish(id, "alice", "", []Entry{{Link: replacement}}, false)
	if err != nil || newToken != "" {
		t.Fatal("refresh unexpectedly rotated the token")
	}
	w = request(handler, http.MethodGet, path)
	decoded, _ = base64.StdEncoding.DecodeString(w.Body.String())
	if w.Code != http.StatusOK || string(decoded) != replacement+"\n" {
		t.Fatal("existing URL did not receive the replaced node")
	}
	_, rotated, err := store.Publish(id, "alice", "", []Entry{{Link: replacement}}, true)
	if err != nil || rotated == "" || rotated == token {
		t.Fatal("token rotation failed")
	}
	if request(handler, http.MethodGet, path).Code != http.StatusNotFound {
		t.Fatal("previous token remains valid after rotation")
	}
	newPath := "/sub/" + id + "/" + rotated
	if request(handler, http.MethodGet, newPath).Code != http.StatusOK {
		t.Fatal("rotated token was not accepted")
	}
	if err := store.Revoke(id); err != nil {
		t.Fatal(err)
	}
	if request(handler, http.MethodGet, newPath).Code != http.StatusNotFound {
		t.Fatal("revoked subscription is still available")
	}
}

func TestDeliveryFiltersExpiryAndEmptyUpdates(t *testing.T) {
	_, store, handler := openFixture(t)
	now := time.Now().UTC()
	expires := now.Add(time.Hour)
	id, token, err := store.Publish("", "alice", "", []Entry{{Link: fixtureLink, ExpiresAt: &expires}}, false)
	if err != nil {
		t.Fatal(err)
	}
	path := "/sub/" + id + "/" + token
	handler.now = func() time.Time { return expires }
	if request(handler, http.MethodGet, path).Code != http.StatusNotFound {
		t.Fatal("expired credentials were delivered")
	}
	handler.now = func() time.Time { return now }
	if request(handler, http.MethodGet, path).Code != http.StatusOK {
		t.Fatal("unexpired subscription is unavailable")
	}
	if _, _, err := store.Publish(id, "alice", "", nil, false); err != nil {
		t.Fatal(err)
	}
	if request(handler, http.MethodGet, path).Code != http.StatusNotFound {
		t.Fatal("empty refresh retained stale credentials")
	}
	if _, _, err := store.Publish(id, "alice", "", []Entry{{Link: fixtureLink}}, false); err != nil {
		t.Fatal(err)
	}
	if request(handler, http.MethodGet, path).Code != http.StatusOK {
		t.Fatal("re-enabled subscription lost its original URL")
	}
}

func TestDeliveryRejectsTraversalSymlinksAndUntrustedRecords(t *testing.T) {
	dir, store, handler := openFixture(t)
	id, token, err := store.Publish("", "alice", "", []Entry{{Link: fixtureLink}}, false)
	if err != nil {
		t.Fatal(err)
	}
	path := "/sub/" + id + "/" + token
	for _, invalid := range []string{"/", "/sub/../secret", "/sub/%2e%2e/secret", "/sub/" + id + "/bad", "/sub/" + id + "/" + base64.RawURLEncoding.EncodeToString(make([]byte, 32)), "/" + id + ".json"} {
		w := request(handler, http.MethodGet, invalid)
		if w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), "vless") || strings.Contains(w.Body.String(), token) {
			t.Fatal("invalid request disclosed subscription data")
		}
	}
	if request(handler, http.MethodPost, path).Code != http.StatusMethodNotAllowed {
		t.Fatal("delivery accepts a mutating method")
	}
	if w := request(handler, http.MethodHead, path); w.Code != http.StatusOK || w.Body.Len() != 0 {
		t.Fatal("HEAD returned a body or rejected the token")
	}
	if w := request(handler, http.MethodGet, "/health"); w.Code != 200 || w.Body.String() != "ok\n" {
		t.Fatal("invalid health response")
	}
	file := filepath.Join(dir, id+".json")
	for _, contents := range []string{"{bad-json", strings.Repeat("x", maxBundleBytes+1)} {
		if err := os.WriteFile(file, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		if request(handler, http.MethodGet, path).Code != http.StatusNotFound {
			t.Fatal("malformed or oversized bundle was served")
		}
	}
	outside := filepath.Join(t.TempDir(), "private.json")
	if err := os.WriteFile(outside, []byte(`{"private":"never serve"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, file); err != nil {
		t.Fatal(err)
	}
	if request(handler, http.MethodGet, path).Code != http.StatusNotFound {
		t.Fatal("delivery followed a bundle symlink")
	}
}

func TestPublisherLockPreventsRefreshAndRevokeRaces(t *testing.T) {
	dir, store, handler := openFixture(t)
	id, token, err := store.Publish("", "alice", "", []Entry{{Link: fixtureLink}}, false)
	if err != nil {
		t.Fatal(err)
	}
	other, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	unlock, err := store.lock(id)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if _, _, err := other.Publish(id, "alice", "", nil, true); err == nil {
		t.Fatal("refresh ignored an active writer lock")
	}
	if err := other.Revoke(id); err == nil {
		t.Fatal("revocation ignored an active writer lock")
	}
	if request(handler, http.MethodGet, "/sub/"+id+"/"+token).Code != http.StatusOK {
		t.Fatal("a failed concurrent operation changed the subscription")
	}
}

func TestAtomicRefreshWhileServing(t *testing.T) {
	_, store, handler := openFixture(t)
	id, token, err := store.Publish("", "alice", "", []Entry{{Link: fixtureLink}}, false)
	if err != nil {
		t.Fatal(err)
	}
	path := "/sub/" + id + "/" + token
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 20 {
				w := request(handler, http.MethodGet, path)
				data, err := base64.StdEncoding.DecodeString(w.Body.String())
				if w.Code != http.StatusOK || err != nil || !strings.HasPrefix(string(data), "vless://") {
					t.Error("reader observed an incomplete subscription update")
				}
			}
		})
	}
	for range 10 {
		if _, _, err := store.Publish(id, "alice", "", []Entry{{Link: fixtureLink}}, false); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
}

func TestFormatsAndURLValidation(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	entries := []Entry{{Link: fixtureLink}, {Link: strings.ReplaceAll(fixtureLink, "vpn.example.net", "expired.example.net"), ExpiresAt: &past}}
	raw, err := Encode(entries, time.Now(), "links")
	if err != nil || string(raw) != fixtureLink+"\n" {
		t.Fatal("plain export retained an expired profile")
	}
	if _, err := Encode(entries, time.Now(), "invalid"); err == nil {
		t.Fatal("invalid format accepted")
	}
	if _, err := Encode([]Entry{{Link: "vless://sensitive-secret"}}, time.Now(), "base64"); err == nil || strings.Contains(err.Error(), "sensitive-secret") {
		t.Fatal("invalid credentials accepted or disclosed in an error")
	}
	for _, raw := range []string{"https://subscriptions.example.net", "http://127.0.0.1:8088", "http://[::1]:8088"} {
		if err := ValidateBaseURL(raw); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range []string{"http://subscriptions.example.net", "https://user:secret@example.net", "https://example.net/sub", "https://example.net/?secret=value", "https://example.net/#secret", "https://example.net?"} {
		if err := ValidateBaseURL(raw); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("invalid or secret-bearing origin was accepted or echoed")
		}
	}
}

func TestServeTLSAndGracefulShutdown(t *testing.T) {
	dir, store, _ := openFixture(t)
	id, token, err := store.Publish("", "alice", "", []Entry{{Link: fixtureLink}}, false)
	if err != nil {
		t.Fatal(err)
	}
	fixture := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	certificate := fixture.TLS.Certificates[0]
	roots := x509.NewCertPool()
	roots.AddCert(fixture.Certificate())
	fixture.Close()
	keyDER, err := x509.MarshalPKCS8PrivateKey(certificate.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	certFile, keyFile := filepath.Join(t.TempDir(), "cert.pem"), filepath.Join(t.TempDir(), "key.pem")
	for path, data := range map[string][]byte{certFile: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]}), keyFile: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, ServeOptions{Dir: dir, Listen: address, CertFile: certFile, KeyFile: keyFile})
	}()
	client := fixture.Client()
	client.Timeout = time.Second
	client.Transport.(*http.Transport).TLSClientConfig.RootCAs = roots
	defer client.CloseIdleConnections()
	deadline := time.Now().Add(3 * time.Second)
	var response *http.Response
	for time.Now().Before(deadline) {
		response, err = client.Get("https://" + address + "/sub/" + id + "/" + token)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal("TLS delivery did not start")
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || len(body) == 0 {
		t.Fatal("TLS subscription request failed")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("subscription listener did not stop")
	}
}

func TestPublicListenerRequiresTLS(t *testing.T) {
	for _, address := range []string{":8088", "0.0.0.0:8088", "[::]:8088", "localhost:8088"} {
		if err := (ServeOptions{Dir: "bundles", Listen: address}).Validate(); err == nil {
			t.Errorf("plaintext listener accepted: %s", address)
		}
	}
	for _, opts := range []ServeOptions{{Dir: "bundles", Listen: "127.0.0.1:8088"}, {Dir: "bundles", Listen: "0.0.0.0:8088", CertFile: "cert", KeyFile: "key"}} {
		if err := opts.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	if err := (ServeOptions{Dir: "bundles", Listen: "127.0.0.1:8088", CertFile: "cert"}).Validate(); err == nil {
		t.Fatal("incomplete TLS options accepted")
	}
	_, store, _ := openFixture(t)
	if _, _, err := store.Publish("", "alice", "bad\r\nheader", []Entry{{Link: fixtureLink}}, false); err == nil {
		t.Fatal("header injection accepted")
	}
	if err := store.Revoke("../outside"); err == nil {
		t.Fatal("invalid subscription id accepted")
	}
}
