//go:build !darwin && !linux && !windows

package observation

import "errors"

func Lock(string) (func(), error) {
	return nil, errors.New("observation is unsupported on this platform")
}
