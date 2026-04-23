package model

import "time"

const (
	ServerRoleVPN   = "vpn"
	ServerRoleProxy = "proxy"
)

type SSHConfig struct {
	User            string `json:"user"`
	Port            int    `json:"port"`
	IdentityFile    string `json:"identity_file"`
	KnownHostsFile  string `json:"known_hosts_file"`
	StrictHostKey   bool   `json:"strict_host_key"`
	ConnectTimeoutS int    `json:"connect_timeout_s"`
}

type Server struct {
	ID                int64      `json:"id"`
	Name              string     `json:"name"`
	Role              string     `json:"role"`
	Host              string     `json:"host"`
	Domain            string     `json:"domain"`
	SSHUser           string     `json:"ssh_user"`
	SSHPort           int        `json:"ssh_port"`
	SSHIdentityFile   string     `json:"ssh_identity_file"`
	SSHKnownHostsFile string     `json:"ssh_known_hosts_file"`
	SSHStrictHostKey  bool       `json:"ssh_strict_host_key"`
	XrayVersion       string     `json:"xray_version"`
	RealityPrivateKey string     `json:"reality_private_key"`
	RealityPublicKey  string     `json:"reality_public_key"`
	RealityShortIDs   string     `json:"reality_short_ids"`
	RealityServerName string     `json:"reality_server_name"`
	RealityTarget     string     `json:"reality_target"`
	ProxyServiceUUID  string     `json:"proxy_service_uuid"`
	Enabled           bool       `json:"enabled"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	LastDeployAt      *time.Time `json:"last_deploy_at,omitempty"`
}

type ProxyBackend struct {
	ID              int64     `json:"id"`
	ProxyServerID   int64     `json:"proxy_server_id"`
	BackendServerID int64     `json:"backend_server_id"`
	Enabled         bool      `json:"enabled"`
	Priority        int       `json:"priority"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	BackendServer   *Server   `json:"backend_server,omitempty"`
}

// NormalizedRole returns the normalized server role.
func (s Server) NormalizedRole() string {
	return NormalizeServerRole(s.Role)
}

// IsProxy reports whether server role is proxy.
func (s Server) IsProxy() bool {
	return s.NormalizedRole() == ServerRoleProxy
}

// IsVPN reports whether server role is vpn.
func (s Server) IsVPN() bool {
	return s.NormalizedRole() == ServerRoleVPN
}

type User struct {
	ID               int64      `json:"id"`
	ServerID         int64      `json:"server_id"`
	Username         string     `json:"username"`
	UUID             string     `json:"uuid"`
	Email            string     `json:"email"`
	Enabled          bool       `json:"enabled"`
	ExpiryDate       *time.Time `json:"expiry_date,omitempty"`
	TrafficLimitByte *int64     `json:"traffic_limit_byte,omitempty"`
	QuotaEnabled     bool       `json:"quota_enabled"`
	QuotaBlocked     bool       `json:"quota_blocked"`
	QuotaBlockedAt   *time.Time `json:"quota_blocked_at,omitempty"`
	Notes            string     `json:"notes"`
	TagsCSV          string     `json:"tags_csv"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type DeployRevision struct {
	ID          int64     `json:"id"`
	ServerID    int64     `json:"server_id"`
	Revision    string    `json:"revision"`
	ConfigHash  string    `json:"config_hash"`
	AppliedBy   string    `json:"applied_by"`
	AppliedAt   time.Time `json:"applied_at"`
	Status      string    `json:"status"`
	Description string    `json:"description"`
}

type BackupRecord struct {
	ID         int64     `json:"id"`
	ServerID   int64     `json:"server_id"`
	Type       string    `json:"type"`
	Path       string    `json:"path"`
	SHA256     string    `json:"sha256"`
	CreatedAt  time.Time `json:"created_at"`
	CreatedBy  string    `json:"created_by"`
	RemotePath string    `json:"remote_path"`
}

type UserTraffic struct {
	ServerID      int64     `json:"server_id"`
	Email         string    `json:"email"`
	WindowStart   time.Time `json:"window_start"`
	WindowType    string    `json:"window_type"`
	UplinkBytes   int64     `json:"uplink_bytes"`
	DownlinkBytes int64     `json:"downlink_bytes"`
}

type LinkOptions struct {
	Fragment string
	Label    string
}

type QuotaUserPolicy struct {
	Email            string `json:"email"`
	UUID             string `json:"uuid"`
	InboundTag       string `json:"inbound_tag"`
	QuotaEnabled     bool   `json:"quota_enabled"`
	MonthlyQuotaByte *int64 `json:"monthly_quota_byte,omitempty"`
}

type UserPolicy struct {
	Username   string     `json:"username"`
	Email      string     `json:"email"`
	UUID       string     `json:"uuid"`
	Enabled    bool       `json:"enabled"`
	ExpiryAt   *time.Time `json:"expiry_at,omitempty"`
	InboundTag string     `json:"inbound_tag"`
}

type QuotaUserStatus struct {
	Email              string     `json:"email"`
	QuotaEnabled       bool       `json:"quota_enabled"`
	Window30DQuotaByte int64      `json:"window_30d_quota_byte"`
	Window30DUsageByte int64      `json:"window_30d_usage_byte"`
	BlockedByQuota     bool       `json:"blocked_by_quota"`
	BlockedAt          *time.Time `json:"blocked_at,omitempty"`
	InboundTag         string     `json:"inbound_tag,omitempty"`
	HasRuntimeIdentity bool       `json:"has_runtime_identity"`
}

type QuotaStatusResponse struct {
	Window30DStart    string            `json:"window_30d_start"`
	Window30DEnd      string            `json:"window_30d_end"`
	DefaultQuotaByte  int64             `json:"default_quota_byte"`
	QuotaEnabledUsers int               `json:"quota_enabled_users"`
	BlockedUsers      int               `json:"blocked_users"`
	Users             []QuotaUserStatus `json:"users"`
}

type UserAccessStatus struct {
	Username           string     `json:"username"`
	Email              string     `json:"email"`
	UUID               string     `json:"uuid"`
	Enabled            bool       `json:"enabled"`
	ExpiryAt           *time.Time `json:"expiry_at,omitempty"`
	ExpiryDate         string     `json:"expiry_date,omitempty"`
	Expired            bool       `json:"expired"`
	EffectiveEnabled   bool       `json:"effective_enabled"`
	DaysUntilExpiry    *float64   `json:"days_until_expiry,omitempty"`
	InboundTag         string     `json:"inbound_tag,omitempty"`
	QuotaEnabled       bool       `json:"quota_enabled"`
	Window30DQuotaByte int64      `json:"window_30d_quota_byte"`
	Window30DUsageByte int64      `json:"window_30d_usage_byte"`
	BlockedByQuota     bool       `json:"blocked_by_quota"`
	BlockedAt          *time.Time `json:"blocked_at,omitempty"`
	HasRuntimeIdentity bool       `json:"has_runtime_identity"`
}

type UserStatusResponse struct {
	Time                  string             `json:"time"`
	EffectiveEnabledUsers int                `json:"effective_enabled_users"`
	Expiring2DUsers       int                `json:"expiring_2d_users"`
	ExpiredUsers          int                `json:"expired_users"`
	Users                 []UserAccessStatus `json:"users"`
}
