package app

import (
	"context"
	"errors"
	"os"
	"time"
	"unicode/utf8"

	"ovpn/internal/app/dto"
	"ovpn/internal/store/local"
)

func (s ReadService) Users(ctx context.Context, cursor string, limit int) (dto.UserPage, error) {
	result := dto.UserPage{Items: make([]dto.User, 0)}
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 || len(cursor) > 4096 || !utf8.ValidString(cursor) {
		return result, &Error{Code: "INVALID_PARAMS"}
	}
	st, err := local.OpenReadOnly(ctx, s.DataDir)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, &Error{Code: "WORKSPACE_UNAVAILABLE"}
	}
	defer st.Close()
	users, err := st.ListUserInventory(ctx, cursor, limit+1, time.Now().UTC())
	if err != nil {
		return result, &Error{Code: "WORKSPACE_UNAVAILABLE"}
	}
	more := len(users) > limit
	if more {
		users = users[:limit]
	}
	for _, u := range users {
		result.Items = append(result.Items, dto.User{Username: u.Username, ConfiguredNodes: u.ConfiguredNodes, EnabledNodes: u.EnabledNodes,
			EligibleNodes: u.EligibleNodes, DisabledNodes: u.DisabledNodes, ExpiredNodes: u.ExpiredNodes, BlockedNodes: u.BlockedNodes,
			EarliestExpiry: u.EarliestExpiry, QuotaMinBytes: u.QuotaMinBytes, QuotaMaxBytes: u.QuotaMaxBytes, PolicyDiff: u.PolicyDiff, IdentityConflict: u.IdentityConflict})
	}
	if more {
		result.NextCursor = result.Items[len(result.Items)-1].Username
	}
	return result, nil
}
