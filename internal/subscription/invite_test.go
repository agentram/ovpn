package subscription

import (
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
