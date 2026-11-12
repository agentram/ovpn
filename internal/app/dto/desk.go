package dto

import (
	"ovpn/internal/haproxy/api"
	"time"
)

// Live fields are nullable so an unavailable agent never becomes a zero reading.
type UserLive struct {
	AccessAt       string `json:"accessAt,omitempty"`
	NextCheckAt    string `json:"nextCheckAt,omitempty"`
	RoutesTotal    int    `json:"routesTotal"`
	RoutesPassed   int    `json:"routesPassed"`
	RoutesFailed   int    `json:"routesFailed"`
	ServerID       string `json:"serverId"`
	Username       string `json:"username"`
	BlockedByQuota *bool  `json:"blockedByQuota"`
	Connected      *bool  `json:"connected"`
	PresenceAt     string `json:"presenceAt,omitempty"`
	PresenceCode   string `json:"presenceCode,omitempty"`
	TrafficAt      string `json:"trafficAt,omitempty"`
	UsedBytes      *int64 `json:"usedBytes"`
	QuotaBytes     *int64 `json:"quotaBytes"`
	RemainingBytes *int64 `json:"remainingBytes"`
	QuotaEnabled   *bool  `json:"quotaEnabled"`
	RuntimeApplied *bool  `json:"runtimeApplied"`
}
type ServerLive struct {
	HA           *api.Status `json:"ha,omitempty"`
	ServerID     string      `json:"serverId"`
	State        string      `json:"state"`
	Code         string      `json:"code,omitempty"`
	CheckedAt    string      `json:"checkedAt,omitempty"`
	AgentVersion string      `json:"agentVersion,omitempty"`
	Users        []UserLive  `json:"users"`
}
type RouteLive struct {
	Target ObservationTarget `json:"target"`
	State  string            `json:"state"`
	Sample *Observation      `json:"sample,omitempty"`
	NextAt time.Time         `json:"nextAt"`
}
type DeskSnapshot struct {
	Enabled     bool         `json:"enabled"`
	Running     bool         `json:"running"`
	Servers     []ServerLive `json:"servers"`
	Routes      []RouteLive  `json:"routes"`
	Checked     int          `json:"checked"`
	Total       int          `json:"total"`
	Active      int          `json:"active"`
	LastRefresh string       `json:"lastRefresh,omitempty"`
}

type DeskConfiguration struct {
	Enabled          bool `json:"enabled"`
	TrafficConfirmed bool `json:"trafficConfirmed"`
}
type RouteQuery struct {
	ServerID string `json:"serverId"`
	Username string `json:"username"`
	Cursor   string `json:"cursor"`
	Limit    int    `json:"limit"`
}
type RoutePage struct {
	Items      []RouteLive `json:"items"`
	NextCursor string      `json:"nextCursor,omitempty"`
}
