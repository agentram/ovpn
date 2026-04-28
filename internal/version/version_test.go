package version

import "testing"

func TestValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{name: "plain semver", in: "1.2.1"},
		{name: "trim spaces", in: " 1.2.3 "},
		{name: "reject v prefix", in: "v1.2.3", wantErr: true},
		{name: "reject suffix", in: "1.2.3-beta", wantErr: true},
		{name: "reject empty", in: "", wantErr: true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.in)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error for %q", tt.in)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate(%q): %v", tt.in, err)
			}
		})
	}
}

func TestTopChangelogVersion(t *testing.T) {
	t.Parallel()

	raw := "# Changelog\n\n## 1.3.0\n\n### Added\n- item\n\n## 1.2.1\n"
	got, err := TopChangelogVersion(raw)
	if err != nil {
		t.Fatalf("TopChangelogVersion: %v", err)
	}
	if got != "1.3.0" {
		t.Fatalf("top changelog version = %q, want %q", got, "1.3.0")
	}
}
