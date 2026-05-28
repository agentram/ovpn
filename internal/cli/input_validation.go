package cli

import (
	"fmt"
	"strings"
)

func requiredFlagValue(name string, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return trimmed, nil
}
