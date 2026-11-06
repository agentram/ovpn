package dto

// UserPlanRequest describes intent only; it is never an instruction to execute it.
type UserPlanRequest struct {
	Action         string `json:"action"`
	Username       string `json:"username"`
	Expiry         string `json:"expiry,omitempty"`
	QuotaBytes     *int64 `json:"quotaBytes,omitempty"`
	ExpectedDigest string `json:"expectedDigest,omitempty"`
}

type UserPolicy struct {
	Enabled      bool   `json:"enabled"`
	ExpiresAt    string `json:"expiresAt,omitempty"`
	QuotaBytes   *int64 `json:"quotaBytes,omitempty"`
	QuotaEnabled bool   `json:"quotaEnabled"`
	QuotaBlocked bool   `json:"quotaBlocked"`
}

type UserPlanTarget struct {
	ServerID string      `json:"serverId"`
	Name     string      `json:"name"`
	Role     string      `json:"role"`
	Profiles []string    `json:"profiles"`
	Before   *UserPolicy `json:"before"`
	After    *UserPolicy `json:"after"`
	Issues   []string    `json:"issues"`
}

// UserPlan is a consistent local snapshot. It makes no remote readiness claim.
type UserPlan struct {
	Action             string           `json:"action"`
	Username           string           `json:"username"`
	Scope              string           `json:"scope"`
	Digest             string           `json:"digest"`
	GeneratedAt        string           `json:"generatedAt"`
	ExecutionAvailable bool             `json:"executionAvailable"`
	Targets            []UserPlanTarget `json:"targets"`
	Issues             []string         `json:"issues"`
	RequiredSteps      []string         `json:"requiredSteps"`
}
