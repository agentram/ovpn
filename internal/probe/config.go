package probe

import (
	"context"
	"ovpn/internal/clientcfg"

	"github.com/xtls/xray-core/core"
)

func CoreVersion() string { return core.Version() }

func CheckConfig(ctx context.Context, config clientcfg.Config, opts Options) (Result, error) {
	if err := opts.Validate(); err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	tunnel, err := Open(ctx, config)
	if err != nil {
		return Result{}, err
	}
	defer tunnel.Close()
	return Check(ctx, opts, tunnel.DialContext)
}
