package subscription

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeliveryVerificationRequiresTrustedTLSAndTheExactPublishedSnapshot(t *testing.T) {
	body := "published-snapshot"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			_, _ = w.Write([]byte("ok\n"))
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	if e := VerifyDelivery(context.Background(), server.URL, server.URL+"/profile", []byte(body)); e == nil {
		t.Fatal("untrusted delivery certificate was accepted")
	}
	if e := verifyDelivery(context.Background(), server.URL, server.URL+"/profile", []byte(body), server.Client()); e != nil {
		t.Fatal(e)
	}
	if e := verifyDelivery(context.Background(), server.URL, server.URL+"/profile", []byte("other-snapshot"), server.Client()); e == nil {
		t.Fatal("unrelated delivery workspace accepted")
	}
	body = ""
	if e := verifyDelivery(context.Background(), server.URL, server.URL+"/profile", []byte("snapshot"), server.Client()); e == nil {
		t.Fatal("empty subscription accepted")
	}
}

func TestActivationPreflightRequiresMountedPostRoute(t *testing.T) {
	for _, fixture := range []struct {
		name      string
		invites   bool
		blockPOST bool
	}{
		{name: "health without invitations"},
		{name: "proxy blocks POST", invites: true, blockPOST: true},
		{name: "activation enabled", invites: true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			_, _, handler := openFixture(t)
			if fixture.invites {
				var err error
				handler.invites, err = OpenInvites(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
			}
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if fixture.blockPOST && r.Method == http.MethodPost {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				handler.ServeHTTP(w, r)
			}))
			defer server.Close()
			client := server.Client()
			if err := verifyDelivery(t.Context(), server.URL, server.URL+"/health", []byte("ok"), client); err != nil {
				t.Fatal("health route failed", err)
			}
			if err := verifyRoute(t.Context(), client, http.MethodGet, server.URL+"/sub/probe/invalid", deliveryHeader, nil); err != nil {
				t.Fatal("subscription route was unavailable", err)
			}
			err := verifyActivation(t.Context(), server.URL, client)
			if (err == nil) != (fixture.invites && !fixture.blockPOST) {
				t.Fatalf("activation preflight = %v", err)
			}
		})
	}
}
