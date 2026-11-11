package app

import (
	"context"
	"errors"
	"os"
	"strconv"
	"time"
	"unicode/utf8"

	"ovpn/internal/app/dto"
	"ovpn/internal/store/local"
)

func (s ReadService) CheckUsers(ctx context.Context, server, cursor string, limit int) (dto.CheckUserPage, error) {
	result := dto.CheckUserPage{Items: []string{}}
	id, err := serverID(server)
	if limit == 0 {
		limit = 50
	}
	if err != nil || limit < 1 || limit > 100 || len(cursor) > 4096 || !utf8.ValidString(cursor) {
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
	result.Items, err = st.ListCheckUsernames(ctx, id, cursor, limit+1, time.Now())
	if err != nil {
		return result, &Error{Code: "WORKSPACE_UNAVAILABLE"}
	}
	if len(result.Items) > limit {
		result.Items = result.Items[:limit]
		result.NextCursor = result.Items[limit-1]
	}
	return result, nil
}

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
	assignments, err := st.UserAssignmentIDs(ctx)
	if err != nil {
		return result, &Error{Code: "WORKSPACE_UNAVAILABLE"}
	}
	servers, err := st.ListServers(ctx)
	if err != nil {
		return result, &Error{Code: "WORKSPACE_UNAVAILABLE"}
	}
	enabled := map[int64]bool{}
	for _, server := range servers {
		if server.Enabled {
			enabled[server.ID] = true
		}
	}
	for _, u := range users {
		missing := len(enabled)
		for _, id := range assignments[u.Username] {
			if enabled[id] {
				missing--
			}
		}
		nodes := []string{}
		for _, id := range assignments[u.Username] {
			nodes = append(nodes, "node-"+strconv.FormatInt(id, 10))
		}
		result.Items = append(result.Items, dto.User{NodeIDs: nodes, MissingNodes: missing, Username: u.Username, ConfiguredNodes: u.ConfiguredNodes, EnabledNodes: u.EnabledNodes,
			EligibleNodes: u.EligibleNodes, DisabledNodes: u.DisabledNodes, ExpiredNodes: u.ExpiredNodes, BlockedNodes: u.BlockedNodes,
			EarliestExpiry: u.EarliestExpiry, QuotaMinBytes: u.QuotaMinBytes, QuotaMaxBytes: u.QuotaMaxBytes, DefaultQuotaNodes: u.DefaultQuotaNodes, UnmeteredNodes: u.UnmeteredNodes, PolicyDiff: u.PolicyDiff, IdentityConflict: u.IdentityConflict})
	}
	if more {
		result.NextCursor = result.Items[len(result.Items)-1].Username
	}
	return result, nil
}
