package model

import (
	"fmt"
	"strconv"
	"strings"
)

// ValidateRealityMinClientVersion checks Xray's three-byte client version format.
// Empty leaves the minimum version to the selected Xray release.
func ValidateRealityMinClientVersion(version string) error {
	if version == "" {
		return nil
	}
	parts := strings.Split(version, ".")
	invalid := func() error {
		return fmt.Errorf("reality_min_client_version must be x.y.z with each component between 0 and 255, or empty for the Xray default")
	}
	if len(parts) != 3 {
		return invalid()
	}
	for _, part := range parts {
		if part == "" {
			return invalid()
		}
		for _, ch := range part {
			if ch < '0' || ch > '9' {
				return invalid()
			}
		}
		if _, err := strconv.ParseUint(part, 10, 8); err != nil {
			return invalid()
		}
	}
	return nil
}
