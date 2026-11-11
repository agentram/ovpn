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
