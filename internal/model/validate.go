package model

import (
	"errors"
	"net/mail"
	"strings"
)

// NormalizeServerRole normalizes role and applies fallback defaults.
func NormalizeServerRole(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", ServerRoleVPN:
		return ServerRoleVPN
	case ServerRoleProxy:
		return ServerRoleProxy
	default:
		return ""
	}
}

// Validate executes validate flow and returns the first error.
func (s Server) Validate() error {
	var errs []string

	role := NormalizeServerRole(s.Role)
	if role == "" {
		errs = append(errs, "role must be \"vpn\" or \"proxy\"")
	}

	if strings.TrimSpace(s.Name) == "" {
		errs = append(errs, "name is required")
	}
	if strings.TrimSpace(s.Host) == "" {
		errs = append(errs, "host is required")
	}
	if strings.TrimSpace(s.Domain) == "" {
		errs = append(errs, "domain is required")
	}
	if strings.TrimSpace(s.SSHUser) == "" {
		errs = append(errs, "ssh_user is required")
	}
	if s.SSHPort < 1 || s.SSHPort > 65535 {
		errs = append(errs, "ssh_port must be between 1 and 65535")
	}
	if strings.TrimSpace(s.XrayVersion) == "" {
		errs = append(errs, "xray_version is required")
	}
	if strings.TrimSpace(s.RealityPrivateKey) == "" {
		errs = append(errs, "reality_private_key is required")
	}
	if strings.TrimSpace(s.RealityPublicKey) == "" {
		errs = append(errs, "reality_public_key is required")
	}
	if strings.TrimSpace(s.RealityShortIDs) == "" {
		errs = append(errs, "reality_short_ids is required")
	}
	if strings.TrimSpace(s.RealityServerName) == "" {
		errs = append(errs, "reality_server_name is required")
	} else if strings.Contains(s.RealityServerName, "*") {
		// REALITY does not support wildcard serverNames and strict values reduce probing surface.
		errs = append(errs, "reality_server_name must not contain wildcard '*'")
	}
	if strings.TrimSpace(s.RealityTarget) == "" {
		errs = append(errs, "reality_target is required")
	}
	if len(errs) == 0 {
		return nil
	}
	return errors.New(strings.Join(errs, "; "))
}

// Validate executes validate flow and returns the first error.
func (u User) Validate() error {
	var errs []string

	if u.ServerID <= 0 {
		errs = append(errs, "server_id is required")
	}
	if strings.TrimSpace(u.Username) == "" {
		errs = append(errs, "username is required")
	}
	if strings.TrimSpace(u.UUID) == "" {
		errs = append(errs, "uuid is required")
	}
	if strings.TrimSpace(u.Email) == "" {
		errs = append(errs, "email is required")
	} else if _, err := mail.ParseAddress(u.Email); err != nil {
		errs = append(errs, "email is invalid")
	}
	if u.TrafficLimitByte != nil && *u.TrafficLimitByte < 0 {
		errs = append(errs, "traffic_limit_byte must be >= 0")
	}

	if len(errs) == 0 {
		return nil
	}
	return errors.New(strings.Join(errs, "; "))
}
