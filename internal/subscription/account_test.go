package subscription

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

func TestAccountProviderVerifiesIssuerAudienceSignatureAndEmail(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var issuer string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch req.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer": issuer, "jwks_uri": issuer + "/keys", "id_token_signing_alg_values_supported": []string{"ES256"},
			})
		case "/keys":
			public := jose.JSONWebKey{Key: &key.PublicKey, KeyID: "fixture", Algorithm: "ES256", Use: "sig"}
			_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{public}})
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(server.Close)
	issuer = server.URL
	verify, err := discoverAccountVerifier(oidc.ClientContext(t.Context(), server.Client()), issuer, []string{"android", "ios"})
	if err != nil {
		t.Fatal(err)
	}
	signer, err := jose.NewSigner(jose.SigningKey{
		Algorithm: jose.ES256, Key: jose.JSONWebKey{Key: key, KeyID: "fixture"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, r := range []struct {
		name     string
		issuer   string
		audience jwt.Audience
		party    string
		expires  time.Time
		verified bool
		tamper   bool
		want     bool
	}{
		{"android", issuer, jwt.Audience{"android"}, "", now.Add(time.Minute), true, false, true},
		{"ios", issuer, jwt.Audience{"ios"}, "", now.Add(time.Minute), true, false, true},
		{"ios as authorized party", issuer, jwt.Audience{"android", "ios"}, "ios", now.Add(time.Minute), true, false, true},
		{"ambiguous client", issuer, jwt.Audience{"android", "ios"}, "", now.Add(time.Minute), true, false, false},
		{"foreign audience", issuer, jwt.Audience{"other"}, "", now.Add(time.Minute), true, false, false},
		{"foreign issuer", "https://other.example", jwt.Audience{"android"}, "", now.Add(time.Minute), true, false, false},
		{"different party", issuer, jwt.Audience{"android"}, "other", now.Add(time.Minute), true, false, false},
		{"expired", issuer, jwt.Audience{"android"}, "", now.Add(-time.Hour), true, false, false},
		{"unverified email", issuer, jwt.Audience{"android"}, "", now.Add(time.Minute), false, false, false},
		{"tampered signature", issuer, jwt.Audience{"android"}, "", now.Add(time.Minute), true, true, false},
	} {
		t.Run(r.name, func(t *testing.T) {
			token, err := jwt.Signed(signer).Claims(jwt.Claims{
				Issuer: r.issuer, Subject: "fixture-account", Audience: r.audience,
				IssuedAt: jwt.NewNumericDate(now), Expiry: jwt.NewNumericDate(r.expires),
			}).Claims(map[string]any{"email": "Fixture@Example.Test", "email_verified": r.verified, "azp": r.party}).Serialize()
			if err != nil {
				t.Fatal(err)
			}
			if r.tamper {
				sig := base64.RawURLEncoding.EncodeToString(make([]byte, 64))
				token = token[:len(token)-len(sig)] + sig
			}
			subject, err := verify(t.Context(), token)
			if r.want {
				if err != nil || subject.subject != tokenHash(issuer+"\x00fixture-account") || subject.email != "fixture@example.test" {
					t.Fatal("verified identity did not map to the expected account")
				}
			} else if err == nil {
				t.Fatal("accepted an invalid identity")
			}
		})
	}
}
