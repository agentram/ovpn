package model

import "testing"

func TestValidateRealityMinClientVersion(t *testing.T) {
	for _, value := range []string{"", "1.0.0", "1.8.1", "26.3.27", "0.0.0", "255.255.255"} {
		if err := ValidateRealityMinClientVersion(value); err != nil {
			t.Errorf("valid version %q: %v", value, err)
		}
	}
	for _, value := range []string{"default", "1", "1.0", "1.0.0.0", "256.0.0", "1.256.0", "1.0.256", "-1.0.0", "+1.0.0", "1..0", "1.0.", " 1.0.0", "1.0.0 ", "1.a.0"} {
		if err := ValidateRealityMinClientVersion(value); err == nil {
			t.Errorf("invalid version %q accepted", value)
		}
	}
}
