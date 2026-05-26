//go:build !runtimeassets

package runtimeassets

import (
	"errors"
	"io/fs"
)

const LinuxGOOS = "linux"

var (
	ErrNotEmbedded = errors.New("runtime assets are not embedded")
	ErrUnsupported = errors.New("runtime asset is not available")
)

type Asset struct {
	Path string
}

func Open(name, goarch string) (fs.File, Asset, error) {
	return nil, Asset{}, ErrNotEmbedded
}
