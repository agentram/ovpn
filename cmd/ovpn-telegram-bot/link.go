package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"ovpn/internal/model"
	"ovpn/internal/xraycfg"
)

// isOwner reports whether owner.
func (b *bot) isOwner(userID int64) bool {
	if b.cfg.ownerUserID <= 0 {
		return false
	}
	return userID == b.cfg.ownerUserID
}

// buildUserLink builds user link from the current inputs and defaults.
func (b *bot) buildUserLink(ctx context.Context, query string) (string, error) {
	query = strings.TrimSpace(query)
	query = strings.TrimPrefix(query, "@")
	if query == "" {
		return "", errors.New("username or email is required")
	}
	if strings.TrimSpace(b.cfg.linkAddress) == "" || strings.TrimSpace(b.cfg.linkServerName) == "" || strings.TrimSpace(b.cfg.linkPublicKey) == "" || strings.TrimSpace(b.cfg.linkShortID) == "" {
		return "", errors.New("link settings are incomplete on server; run `ovpn deploy <server>` and retry")
	}

	policies, err := b.fetchQuotaPolicies(ctx)
	if err != nil {
		return "", err
	}
	if len(policies) == 0 {
		return "", errors.New("no users are available on server")
	}

	pol, username, err := findPolicyForQuery(query, policies)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(pol.UUID) == "" {
		return "", fmt.Errorf("user %s has empty UUID in quota policy", username)
	}

	link := xraycfg.BuildVLESSLink(xraycfg.LinkInput{
		Address:    b.cfg.linkAddress,
		Port:       443,
		UUID:       pol.UUID,
		ServerName: b.cfg.linkServerName,
		Password:   b.cfg.linkPublicKey,
		ShortID:    strings.TrimSpace(b.cfg.linkShortID),
		Label:      "ovpn-" + username,
	})
	return link, nil
}

// findPolicyForQuery returns policy for query for callers.
func findPolicyForQuery(query string, policies []model.QuotaUserPolicy) (model.QuotaUserPolicy, string, error) {
	query = strings.ToLower(strings.TrimSpace(query))
	query = strings.TrimPrefix(query, "@")
	if query == "" {
		return model.QuotaUserPolicy{}, "", errors.New("username or email is required")
	}

	if strings.Contains(query, "@") {
		for _, p := range policies {
			if strings.EqualFold(strings.TrimSpace(p.Email), query) {
				return p, usernameFromEmail(p.Email), nil
			}
		}
		return model.QuotaUserPolicy{}, "", fmt.Errorf("user %q not found", query)
	}

	var matches []model.QuotaUserPolicy
	for _, p := range policies {
		if strings.EqualFold(usernameFromEmail(p.Email), query) {
			matches = append(matches, p)
		}
	}
	if len(matches) == 0 {
		return model.QuotaUserPolicy{}, "", fmt.Errorf("username %q not found", query)
	}
	if len(matches) > 1 {
		return model.QuotaUserPolicy{}, "", fmt.Errorf("multiple users found for %q, send full email", query)
	}
	return matches[0], usernameFromEmail(matches[0].Email), nil
}

// usernameFromEmail returns username from email.
func usernameFromEmail(email string) string {
	email = strings.TrimSpace(email)
	if i := strings.Index(email, "@"); i > 0 {
		return email[:i]
	}
	return email
}
