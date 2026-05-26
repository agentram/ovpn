//go:build runtimeassets

package runtimeassets

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"
)

const LinuxGOOS = "linux"

var (
	ErrNotEmbedded = errors.New("runtime assets are not embedded")
	ErrUnsupported = errors.New("runtime asset is not available")
)

//go:embed assets/linux_amd64/ovpn-agent assets/linux_amd64/ovpn-telegram-bot assets/linux_arm64/ovpn-agent assets/linux_arm64/ovpn-telegram-bot
var assetFS embed.FS

type Asset struct {
	Path string
}

func Open(name, goarch string) (fs.File, Asset, error) {
	assetPath, ok := resolveAssetPath(name, goarch)
	if !ok {
		return nil, Asset{}, fmt.Errorf("%w: %s linux/%s", ErrUnsupported, strings.TrimSpace(name), strings.TrimSpace(goarch))
	}
	f, err := assetFS.Open(assetPath)
	if err != nil {
		return nil, Asset{}, fmt.Errorf("%w: %s: %v", ErrNotEmbedded, assetPath, err)
	}
	if _, err := f.Stat(); err != nil {
		_ = f.Close()
		return nil, Asset{}, err
	}
	return f, Asset{Path: assetPath}, nil
}

func resolveAssetPath(name, goarch string) (string, bool) {
	name = strings.TrimSpace(name)
	goarch = strings.TrimSpace(goarch)
	switch name {
	case "ovpn-agent", "ovpn-telegram-bot":
	default:
		return "", false
	}
	switch goarch {
	case "amd64", "arm64":
	default:
		return "", false
	}
	return path.Join("assets", "linux_"+goarch, name), true
}
