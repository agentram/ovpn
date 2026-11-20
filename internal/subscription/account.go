package subscription

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

type accountVerifier func(context.Context, string) (string, error)

func openAccountVerifier(ctx context.Context, issuer string, clients []string) (accountVerifier, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	ctx = oidc.ClientContext(ctx, &http.Client{
		Timeout:       8 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	})
	return discoverAccountVerifier(ctx, issuer, clients)
}

func discoverAccountVerifier(ctx context.Context, issuer string, clients []string) (accountVerifier, error) {
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, errors.New("could not load account provider discovery")
	}
	var metadata struct {
		KeysURL string `json:"jwks_uri"`
	}
	if provider.Claims(&metadata) != nil || !strings.HasPrefix(metadata.KeysURL, "https://") {
		return nil, errors.New("account provider signing keys must use HTTPS")
	}
	verifiers := make([]*oidc.IDTokenVerifier, 0, len(clients))
	for _, client := range clients {
		verifiers = append(verifiers, provider.VerifierContext(context.WithoutCancel(ctx), &oidc.Config{
			ClientID: client, SupportedSigningAlgs: []string{oidc.RS256, oidc.ES256},
		}))
	}
	return func(ctx context.Context, raw string) (string, error) {
		for i, verifier := range verifiers {
			token, err := verifier.Verify(ctx, raw)
			if err != nil {
				continue
			}
			var claims struct {
				EmailVerified bool   `json:"email_verified"`
				Party         string `json:"azp"`
			}
			if token.Subject == "" || len(token.Subject) > 255 || token.Claims(&claims) != nil ||
				!claims.EmailVerified || (len(token.Audience) != 1 && claims.Party == "") {
				break
			}
			if claims.Party != "" && claims.Party != clients[i] {
				continue
			}
			// Separate subjects from different issuers without persisting identity data.
			return tokenHash(issuer + "\x00" + token.Subject), nil
		}
		return "", errors.New("account verification failed")
	}, nil
}

func (h *Handler) account(w http.ResponseWriter, r *http.Request) (string, bool) {
	if h.verifyAccount == nil {
		return "", true
	}
	fields := strings.Fields(r.Header.Get("Authorization"))
	if len(fields) == 2 && strings.EqualFold(fields[0], "Bearer") && len(fields[1]) <= 7168 {
		subject, err := h.verifyAccount(r.Context(), fields[1])
		if err == nil && validDigest(subject) {
			return subject, true
		}
	}
	w.Header().Set("WWW-Authenticate", "Bearer")
	w.WriteHeader(http.StatusUnauthorized)
	return "", false
}
