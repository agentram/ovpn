package dto

// CheckUserPage contains only usernames eligible for a check on one server.
type CheckUserPage struct {
	Items      []string `json:"items"`
	NextCursor string   `json:"nextCursor,omitempty"`
}

// User contains configured access policy, not a claim of runtime connectivity.
type User struct {
	Username          string `json:"username"`
	ConfiguredNodes   int    `json:"configuredNodes"`
	EnabledNodes      int    `json:"enabledNodes"`
	EligibleNodes     int    `json:"eligibleNodes"`
	DisabledNodes     int    `json:"disabledNodes"`
	ExpiredNodes      int    `json:"expiredNodes"`
	BlockedNodes      int    `json:"blockedNodes"`
	EarliestExpiry    string `json:"earliestExpiry,omitempty"`
	QuotaMinBytes     int64  `json:"quotaMinBytes"`
	QuotaMaxBytes     int64  `json:"quotaMaxBytes"`
	DefaultQuotaNodes int    `json:"defaultQuotaNodes"`
	UnmeteredNodes    int    `json:"unmeteredNodes"`
	PolicyDiff        bool   `json:"policyDiff"`
	IdentityConflict  bool   `json:"identityConflict"`
}

type UserPage struct {
	Items      []User `json:"items"`
	NextCursor string `json:"nextCursor,omitempty"`
}
