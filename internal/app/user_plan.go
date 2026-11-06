package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/mail"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"ovpn/internal/app/dto"
	"ovpn/internal/model"
	"ovpn/internal/store/local"
)

// PlanUser is shared by the CLI and desktop. Planning neither opens a writable
// control store nor calls a remote host. Applying requires a separate operation engine.
func (s ReadService) PlanUser(ctx context.Context, request dto.UserPlanRequest) (dto.UserPlan, error) {
	bad := &Error{Code: "INVALID_PARAMS"}
	if request.Username == "" || len(request.Username) > 128 || !utf8.ValidString(request.Username) ||
		strings.TrimSpace(request.Username) != request.Username || strings.IndexFunc(request.Username, unicode.IsControl) >= 0 {
		return dto.UserPlan{}, bad
	}
	if request.Action != "create" && request.Action != "enable" && request.Action != "disable" {
		return dto.UserPlan{}, bad
	}
	if request.ExpectedDigest != "" {
		if len(request.ExpectedDigest) != sha256.Size*2 {
			return dto.UserPlan{}, bad
		}
		decoded, err := hex.DecodeString(request.ExpectedDigest)
		if err != nil || len(decoded) != sha256.Size || strings.ToLower(request.ExpectedDigest) != request.ExpectedDigest {
			return dto.UserPlan{}, bad
		}
	}
	var expiry *time.Time
	if request.Action == "create" {
		identity, err := mail.ParseAddress(request.Username + "@global")
		if err != nil || identity.Address != request.Username+"@global" || request.QuotaBytes != nil && (*request.QuotaBytes <= 0 || *request.QuotaBytes > 1<<53-1) {
			return dto.UserPlan{}, bad
		}
		expiry, err = model.ParseExpiryDate(request.Expiry)
		if err != nil || strings.TrimSpace(request.Expiry) != request.Expiry {
			return dto.UserPlan{}, bad
		}
	} else if request.Expiry != "" || request.QuotaBytes != nil {
		return dto.UserPlan{}, bad
	}
	st, err := local.OpenReadOnly(ctx, s.DataDir)
	if err != nil {
		return dto.UserPlan{}, &Error{Code: "WORKSPACE_UNAVAILABLE"}
	}
	defer st.Close()
	targets, revision, conflict, err := st.UserPlanSnapshot(ctx, request.Username)
	if err != nil {
		return dto.UserPlan{}, &Error{Code: "WORKSPACE_UNAVAILABLE"}
	}
	if len(targets) > 100 {
		return dto.UserPlan{}, &Error{Code: "PLAN_TOO_LARGE"}
	}
	expected := request.ExpectedDigest
	request.ExpectedDigest = ""
	raw, err := json.Marshal(struct {
		Schema   int                 `json:"schema"`
		Request  dto.UserPlanRequest `json:"request"`
		Revision string              `json:"revision"`
	}{1, request, revision})
	if err != nil {
		return dto.UserPlan{}, &Error{Code: "INTERNAL_ERROR"}
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	if expected != "" && expected != digest {
		return dto.UserPlan{}, &Error{Code: "STALE_PLAN"}
	}
	plan := dto.UserPlan{Action: request.Action, Username: request.Username, Scope: "all_enabled_servers", Digest: digest,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339), Targets: targets, Issues: []string{},
		RequiredSteps: []string{"transport_and_policy_preflight", "stored_plan_and_writer_locks", "verified_backups", "durable_execution_journal", "per_node_reconciliation", "connection_verification", "subscription_publication"}}
	if len(targets) == 0 {
		plan.Issues = append(plan.Issues, "NO_ENABLED_SERVERS")
	}
	if conflict {
		plan.Issues = append(plan.Issues, "IDENTITY_CONFLICT")
	}
	for i := range plan.Targets {
		target := &plan.Targets[i]
		if request.Action == "create" {
			if target.Before != nil {
				target.Issues = append(target.Issues, "USER_EXISTS")
				continue
			}
			target.After = &dto.UserPolicy{Enabled: true, QuotaEnabled: true, QuotaBytes: request.QuotaBytes}
			if expiry != nil {
				target.After.ExpiresAt = expiry.Format(time.RFC3339)
			}
		} else if target.Before == nil {
			target.Issues = append(target.Issues, "USER_MISSING")
		} else {
			policy := *target.Before
			policy.Enabled = request.Action == "enable"
			target.After = &policy
		}
	}
	return plan, nil
}
