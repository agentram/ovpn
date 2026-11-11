package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"ovpn/internal/app/dto"
	"ovpn/internal/model"
	"ovpn/internal/operations"
	"ovpn/internal/probequeue"
)

// Telemetry batches each server's users. Private agent fields are projected here.
func (d *deskControl) Telemetry(ctx context.Context, presenceOnly bool) ([]dto.ServerLive, error) {
	snap, _, err := d.snapshot(ctx)
	if err != nil {
		return nil, err
	}
	servers := []model.Server{}
	for _, server := range snap.Servers {
		if server.Enabled {
			servers = append(servers, server)
		}
	}
	rows := make([]dto.ServerLive, len(servers))
	var wg sync.WaitGroup
	gate := make(chan struct{}, 4)
	for i, server := range servers {
		wg.Add(1)
		go func(i int, server model.Server) {
			defer wg.Done()
			select {
			case gate <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-gate }()
			target := operations.Target{ID: fmt.Sprintf("node-%d", server.ID), Name: server.Name}
			row := dto.ServerLive{ServerID: target.ID, State: "unknown", Users: []dto.UserLive{}}
			for _, user := range snap.Users {
				if user.ServerID == server.ID {
					row.Users = append(row.Users, dto.UserLive{ServerID: target.ID, Username: user.Username, PresenceCode: "PRESENCE_UNAVAILABLE"})
				}
			}
			nodeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			defer cancel()
			a, err := d.instance(nodeCtx, target, "server.status")
			if err != nil {
				row.Code = "AGENT_UNAVAILABLE"
				rows[i] = row
				return
			}
			defer a.store.Close()
			if !presenceOnly {
				collectedAt := ""
				var coreReachable *bool
				if body, e := a.fetchRemoteAgent(server, "GET", a.agentURL("/health"), nil); e == nil {
					var health struct {
						LastCollectAt    string `json:"last_collect_at"`
						XrayAPIReachable *bool  `json:"xray_api_reachable"`
					}
					if json.Unmarshal(body, &health) == nil {
						collectedAt = health.LastCollectAt
						coreReachable = health.XrayAPIReachable
					}
				}
				var status model.UserStatusResponse
				body, err := a.fetchRemoteAgent(server, "GET", a.agentURL("/users/status"), nil)
				if err == nil && json.Unmarshal(body, &status) == nil && status.Time != "" {
					row.CheckedAt = status.Time
					if coreReachable != nil {
						if *coreReachable {
							row.State = "online"
						} else {
							row.State = "failed"
							row.Code = "XRAY_UNAVAILABLE"
						}
					}
					for i := range row.Users {
						for _, user := range status.Users {
							if user.Username != row.Users[i].Username {
								continue
							}
							live := &row.Users[i]
							used, quota, remaining := user.Window30DUsageByte, user.Window30DQuotaByte, user.Window30DQuotaByte-user.Window30DUsageByte
							if remaining < 0 {
								remaining = 0
							}
							live.TrafficAt = collectedAt
							if collectedAt != "" {
								live.UsedBytes = &used
							}
							live.QuotaEnabled = &user.QuotaEnabled
							live.BlockedByQuota = &user.BlockedByQuota
							live.RuntimeApplied = &user.HasRuntimeIdentity
							if user.QuotaEnabled {
								live.QuotaBytes = &quota
								if collectedAt != "" {
									live.RemainingBytes = &remaining
								}
							}
						}
					}
				} else {
					row.Code = "AGENT_UNAVAILABLE"
					row.State = "unreachable"
					row.CheckedAt = time.Now().UTC().Format(time.RFC3339)
				}
			}
			var presence model.UserPresence
			raw, err := a.fetchRemoteAgent(server, "GET", a.agentURL("/users/online"), nil)
			if err == nil && json.Unmarshal(raw, &presence) == nil {
				online := map[string]bool{}
				for _, email := range presence.Online {
					online[email] = true
				}
				for i := range row.Users {
					live := &row.Users[i]
					if !presence.Supported {
						live.PresenceCode = "AGENT_UPDATE_REQUIRED"
						continue
					}
					for _, user := range snap.Users {
						if user.ServerID == server.ID && user.Username == live.Username {
							if probequeue.Shared.Masked(user.UUID) {
								live.PresenceCode = "CHECK_IN_PROGRESS"
								continue
							}
							value := online[user.Email]
							live.Connected = &value
							live.PresenceAt = presence.SampledAt
							live.PresenceCode = ""
							break
						}
					}
				}
			}
			rows[i] = row
		}(i, server)
	}
	wg.Wait()
	return rows, nil
}
