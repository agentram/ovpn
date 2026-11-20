package subscription

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAuthenticatedActivationBindsProfilesToTheAccount(t *testing.T) {
	_, store, handler := openFixture(t)
	var err error
	handler.invites, err = OpenInvites(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	handler.verifyAccount = func(_ context.Context, token string) (accountIdentity, error) {
		if token == "first" || token == "second" {
			return accountIdentity{subject: tokenHash(token), email: token + "@example.test"}, nil
		}
		return accountIdentity{}, ErrNotFound
	}
	id, _, err := store.Publish("", "alice", "", []Entry{{Link: fixtureLink}}, false)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := store.Read(id)
	if err != nil {
		t.Fatal(err)
	}
	code := issueFixture(t, handler.invites, bundle, time.Now())
	session := newToken()
	body, _ := json.Marshal(map[string]string{"code": code, "session": session})
	path := "/device/" + tokenHash(code) + "/" + session
	for _, step := range []struct {
		name, token, method, path string
		want                      int
	}{
		{"anonymous login", "", "GET", "/account", 401},
		{"account verified", "first", "GET", "/account", 204},
		{"account alone has no access", "first", "GET", "/account/profiles", 404},
		{"anonymous activation", "", "POST", "/activate", 401},
		{"claim", "first", "POST", "/activate", 204},
		{"retry", "first", "POST", "/activate", 204},
		{"other account cannot replay", "second", "POST", "/activate", 404},
		{"invalid token cannot download", "expired", "GET", path, 401},
		{"other account cannot download", "second", "GET", path, 404},
		{"original account can download", "first", "GET", path, 200},
		{"restore without the device receipt", "first", "GET", "/account/profiles", 200},
		{"other account still has no access", "second", "GET", "/account/profiles", 404},
	} {
		t.Run(step.name, func(t *testing.T) {
			req := httptest.NewRequest(step.method, step.path, strings.NewReader(string(body)))
			if step.token != "" {
				req.Header.Set("Authorization", "Bearer "+step.token)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != step.want {
				t.Fatalf("status %d, want %d", w.Code, step.want)
			}
		})
	}
}

func TestActivationDeliversUpdatesAndRevokesWithParent(t *testing.T) {
	_, store, handler := openFixture(t)
	inviteDir := t.TempDir()
	invites, err := OpenInvites(inviteDir)
	if err != nil {
		t.Fatal(err)
	}
	handler.invites = invites
	id, _, err := store.Publish("", "alice", "", []Entry{{Link: fixtureLink}}, false)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := store.Read(id)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	handler.now = func() time.Time { return now }
	code := issueFixture(t, invites, bundle, now)
	sessions := []string{newToken(), newToken()}
	results := make([]int, len(sessions))
	var wg sync.WaitGroup
	for i, session := range sessions {
		wg.Go(func() { results[i] = postActivation(handler, code, session) })
	}
	wg.Wait()
	winner := -1
	for i, result := range results {
		switch result {
		case http.StatusNoContent:
			if winner != -1 {
				t.Fatal("more than one device claimed the invitation")
			}
			winner = i
		case http.StatusNotFound:
		default:
			t.Fatalf("unexpected activation status: %d", result)
		}
	}
	if winner < 0 {
		t.Fatal("no device could activate")
	}
	session := sessions[winner]
	now = now.Add(2 * time.Hour)
	if postActivation(handler, code, session) != http.StatusNoContent {
		t.Fatal("retry after a lost response did not reuse the receipt")
	}
	if postActivation(handler, code, sessions[1-winner]) != http.StatusNotFound {
		t.Fatal("used code was accepted with a different receipt")
	}
	path := "/device/" + tokenHash(code) + "/" + session
	if request(handler, http.MethodGet, path).Code != http.StatusOK {
		t.Fatal("activation did not grant access to the profiles")
	}
	if request(handler, http.MethodGet, "/device/"+tokenHash(code)+"/"+sessions[1-winner]).Code != http.StatusNotFound {
		t.Fatal("a different token opened the device subscription")
	}

	updated := strings.Replace(fixtureLink, "vpn.example.net", "replacement.example.net", 1)
	if _, _, err := store.Publish(id, "alice", "", []Entry{{Link: updated}}, false); err != nil {
		t.Fatal(err)
	}
	w := request(handler, http.MethodGet, path)
	decoded, err := base64.StdEncoding.DecodeString(w.Body.String())
	if w.Code != http.StatusOK || err != nil || string(decoded) != updated+"\n" {
		t.Fatal("the activated device did not receive the updated snapshot")
	}
	for _, name := range []string{tokenHash(code) + ".invite", tokenHash(code) + ".claim"} {
		raw, err := os.ReadFile(inviteDir + "/" + name)
		if err != nil || strings.Contains(string(raw), code) || strings.Contains(string(raw), session) {
			t.Fatal("activation secrets were persisted in plaintext")
		}
	}
	if _, _, err := store.Publish(id, "alice", "", []Entry{{Link: updated}}, true); err != nil {
		t.Fatal(err)
	}
	if request(handler, http.MethodGet, path).Code != http.StatusNotFound {
		t.Fatal("rotation did not invalidate the old device subscription")
	}
}

func TestActivationRejectsExpiredUnclaimedCode(t *testing.T) {
	_, store, handler := openFixture(t)
	var err error
	handler.invites, err = OpenInvites(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id, _, err := store.Publish("", "alice", "", []Entry{{Link: fixtureLink}}, false)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := store.Read(id)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	code := issueFixture(t, handler.invites, bundle, now)
	handler.now = func() time.Time { return now.Add(time.Hour) }
	if postActivation(handler, code, newToken()) != http.StatusNotFound {
		t.Fatal("expired code activated a device")
	}
}

func issueFixture(t *testing.T, invites *Invites, bundle Bundle, now time.Time) string {
	t.Helper()
	raw, err := invites.Issue(bundle, "https://subscriptions.example.net", now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	_, code, ok := strings.Cut(raw, "#")
	if !ok || !validToken(code) {
		t.Fatal("invitation did not contain a valid token")
	}
	return code
}

func postActivation(handler http.Handler, code, session string) int {
	body, _ := json.Marshal(map[string]string{"code": code, "session": session})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/activate", strings.NewReader(string(body))))
	return w.Code
}

func TestLegacyReceiptSurvivesSeparateAccountRollout(t *testing.T) {
	_, legacyStore, legacy := openFixture(t)
	legacyDir := t.TempDir()
	var err error
	legacy.invites, err = OpenInvites(legacyDir)
	if err != nil {
		t.Fatal(err)
	}
	id, _, err := legacyStore.Publish("", "fixture-user", "", []Entry{{Link: fixtureLink}}, false)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := legacyStore.Read(id)
	if err != nil {
		t.Fatal(err)
	}
	code := issueFixture(t, legacy.invites, bundle, time.Now())
	receipt := newToken()
	if postActivation(legacy, code, receipt) != http.StatusNoContent {
		t.Fatal("legacy activation failed")
	}
	path := "/device/" + tokenHash(code) + "/" + receipt

	_, _, account := openFixture(t)
	account.invites, err = OpenInvites(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	account.verifyAccount = func(context.Context, string) (accountIdentity, error) {
		return accountIdentity{subject: tokenHash("fixture-account"), email: "fixture@example.test"}, nil
	}
	if response := request(account, http.MethodGet, path); response.Code != http.StatusUnauthorized {
		t.Fatal("legacy receipt was accepted by the account origin", response.Code)
	}

	// An existing installation refreshes with its original receipt after the
	// legacy service restarts alongside the account origin.
	legacy.invites, err = OpenInvites(legacyDir)
	if err != nil {
		t.Fatal(err)
	}
	if response := request(legacy, http.MethodGet, path); response.Code != http.StatusOK {
		t.Fatal("legacy installation cannot refresh on its original origin", response.Code)
	}
}

func TestVerifiedRecipientAndUniqueEmailAcrossAccounts(t *testing.T) {
	_, store, handler := openFixture(t)
	dir := t.TempDir()
	invites, err := OpenInvites(dir)
	if err != nil {
		t.Fatal(err)
	}
	handler.invites = invites
	handler.requireInviteEmail = true
	handler.verifyAccount = func(_ context.Context, token string) (accountIdentity, error) {
		switch token {
		case "first", "second-device":
			return accountIdentity{subject: tokenHash("first"), email: " Recipient@Example.Test "}, nil
		case "other-provider":
			return accountIdentity{subject: tokenHash("other-provider"), email: "recipient@example.test"}, nil
		case "wrong-recipient":
			return accountIdentity{subject: tokenHash("wrong"), email: "wrong@example.test"}, nil
		case "changed-email":
			return accountIdentity{subject: tokenHash("first"), email: "moved@example.test"}, nil
		default:
			return accountIdentity{}, ErrNotFound
		}
	}
	id, _, err := store.Publish("", "fixture", "", []Entry{{Link: fixtureLink}}, false)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := store.Read(id)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := invites.IssueForEmail(bundle, "https://subscriptions.example.net", time.Now(), time.Hour, "recipient@example.test")
	if err != nil {
		t.Fatal(err)
	}
	_, code, _ := strings.Cut(raw, "#")
	receipt := newToken()
	body, _ := json.Marshal(map[string]string{"code": code, "session": receipt})
	hit := func(method, path, access string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(string(body)))
		if access != "" {
			req.Header.Set("Authorization", "Bearer "+access)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	if status := hit(http.MethodPost, "/activate", "wrong-recipient").Code; status != http.StatusNotFound {
		t.Fatalf("forwarded invitation: HTTP %d", status)
	}
	if status := hit(http.MethodPost, "/activate", "").Code; status != http.StatusUnauthorized {
		t.Fatalf("anonymous recipient: HTTP %d", status)
	}
	if status := hit(http.MethodPost, "/activate", "first").Code; status != http.StatusNoContent {
		t.Fatalf("verified recipient: HTTP %d", status)
	}
	if status := hit(http.MethodGet, "/account/profiles", "second-device").Code; status != http.StatusOK {
		t.Fatalf("second device: HTTP %d", status)
	}
	if status := hit(http.MethodGet, "/account", "other-provider").Code; status != http.StatusConflict {
		t.Fatalf("different subject using the same mailbox: HTTP %d", status)
	}
	if status := hit(http.MethodGet, "/account/profiles", "changed-email").Code; status != http.StatusConflict {
		t.Fatalf("different mailbox using the same subject: HTTP %d", status)
	}
	if token := hit(http.MethodGet, "/account", "first").Header().Get("X-OVPN-Account"); token != tokenHash("first") {
		t.Fatal("missing stable account identifier")
	}
	for _, path := range []string{tokenHash(code) + ".invite", "accounts.db"} {
		data, err := os.ReadFile(dir + "/" + path)
		if err != nil || strings.Contains(string(data), "recipient@example.test") {
			t.Fatal("email was persisted in plaintext")
		}
	}
	if err := invites.Close(); err != nil {
		t.Fatal(err)
	}
	handler.invites, err = OpenInvites(dir)
	if err != nil {
		t.Fatal(err)
	}
	if status := hit(http.MethodGet, "/account/profiles", "second-device").Code; status != http.StatusOK {
		t.Fatalf("assignment was lost after restart: HTTP %d", status)
	}
}

func TestStrictActivationLimitsFailuresAndLeavesExistingReceiptUnchanged(t *testing.T) {
	_, store, handler := openFixture(t)
	var err error
	handler.invites, err = OpenInvites(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	handler.requireInviteEmail = true
	handler.verifyAccount = func(_ context.Context, token string) (accountIdentity, error) {
		return accountIdentity{subject: tokenHash(token), email: token + "@example.test"}, nil
	}
	id, _, err := store.Publish("", "fixture", "", []Entry{{Link: fixtureLink}}, false)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := store.Read(id)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	handler.now = func() time.Time { return now }
	code := issueFixture(t, handler.invites, bundle, now)
	receipt := newToken()
	activate := func(token, code string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"code": code, "session": receipt})
		r := httptest.NewRequest(http.MethodPost, "/activate", strings.NewReader(string(body)))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	for range activationLimit {
		if status := activate("first", code).Code; status != http.StatusNotFound {
			t.Fatalf("strict mode allowed an untargeted code: HTTP %d", status)
		}
	}
	limited := activate("first", code)
	if limited.Code != http.StatusTooManyRequests || limited.Header().Get("Retry-After") == "" {
		t.Fatal("activation was not throttled")
	}
	if status := activate("second", code).Code; status != http.StatusNotFound {
		t.Fatalf("another subject was affected by the limiter: HTTP %d", status)
	}
	now = now.Add(activationWindow)
	if status := activate("first", code).Code; status != http.StatusNotFound {
		t.Fatalf("activation window did not reset: HTTP %d", status)
	}
}

func TestLegacyAccountBindingIsMigratedWithoutReissuingItsCode(t *testing.T) {
	_, store, handler := openFixture(t)
	var err error
	handler.invites, err = OpenInvites(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	subject := tokenHash("legacy")
	handler.verifyAccount = func(context.Context, string) (accountIdentity, error) {
		return accountIdentity{subject: subject, email: "legacy@example.test"}, nil
	}
	handler.requireInviteEmail = true
	id, _, err := store.Publish("", "fixture", "", []Entry{{Link: fixtureLink}}, false)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := store.Read(id)
	if err != nil {
		t.Fatal(err)
	}
	code := issueFixture(t, handler.invites, bundle, time.Now())
	if err := handler.invites.create(subject+".account", []byte(tokenHash(code))); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/account/profiles", nil)
	r.Header.Set("Authorization", "Bearer legacy")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("legacy profile was not preserved: HTTP %d", w.Code)
	}
}
