package app

import (
	"context"

	"ovpn/internal/clientprofile"
	"ovpn/internal/model"
	"ovpn/internal/probe"
)

// CheckClient is shared by the human CLI and machine worker; it never mutates user state.
func CheckClient(ctx context.Context, srv model.Server, user model.User, profile string, opts probe.Options) (probe.Result, error) {
	if err := opts.Validate(); err != nil {
		return probe.Result{}, err
	}
	config, err := clientprofile.ConfigForUser(srv, user, profile, clientprofile.Options{})
	if err != nil {
		return probe.Result{}, err
	}
	return probe.CheckConfig(ctx, config, opts)
}
