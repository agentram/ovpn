package cli

import (
	"fmt"
	"strings"
	"testing"
)

func TestQuotaBytesFromFlagsUsesMonthlyGB(t *testing.T) {
	t.Parallel()

	got, err := quotaBytesFromFlags(0, 400, false, true)
	if err != nil {
		t.Fatalf("quota bytes: %v", err)
	}
	want := int64(400) * quotaGBBytes
	if got != want {
		t.Fatalf("got %d want %d", got, want)
	}
}

func TestQuotaBytesFromFlagsRejectsConflictingInputs(t *testing.T) {
	t.Parallel()

	_, err := quotaBytesFromFlags(1, 1, true, true)
	if err == nil || !strings.Contains(err.Error(), "only one") {
		t.Fatalf("expected conflict error, got %v", err)
	}
}

func TestQuotaBytesFromFlagsRejectsNegativeMonthlyGB(t *testing.T) {
	t.Parallel()

	_, err := quotaBytesFromFlags(0, -1, false, true)
	if err == nil || !strings.Contains(err.Error(), "monthly-gb") {
		t.Fatalf("expected monthly-gb error, got %v", err)
	}
}

func TestQuotaBytesFromFlagsRejectsNegativeMonthlyBytes(t *testing.T) {
	t.Parallel()

	_, err := quotaBytesFromFlags(-1, 0, true, false)
	if err == nil || !strings.Contains(err.Error(), "monthly-bytes") {
		t.Fatalf("expected monthly-bytes error, got %v", err)
	}
}

func TestQuotaBytesFromFlagsRejectsMonthlyGBOverflow(t *testing.T) {
	t.Parallel()

	_, err := quotaBytesFromFlags(0, (1<<63-1)/quotaGBBytes+1, false, true)
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected overflow error, got %v", err)
	}
}

func TestQuotaSetAllowsTrailingGBUnit(t *testing.T) {
	t.Parallel()

	cmd := (&App{}).newUserQuotaSetCmd()
	if err := cmd.ParseFlags([]string{"--username", "alice", "--monthly-gb", "400"}); err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	if err := cmd.Args(cmd, []string{"GB"}); err != nil {
		t.Fatalf("expected trailing GB unit to be accepted, got %v", err)
	}
}

func TestQuotaSetRejectsUnexpectedTrailingUnit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args []string
	}{
		{name: "unknown unit", args: []string{"--username", "alice", "--monthly-gb", "400", "MB"}},
		{name: "gb without monthly-gb", args: []string{"--username", "alice", "--monthly-bytes", "400", "GB"}},
		{name: "extra words", args: []string{"--username", "alice", "--monthly-gb", "400", "GB", "extra"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := (&App{}).newUserQuotaSetCmd()
			if err := cmd.ParseFlags(tc.args); err != nil {
				t.Fatalf("parse flags: %v", err)
			}
			trailing := cmd.Flags().Args()
			if err := cmd.Args(cmd, trailing); err == nil || !strings.Contains(err.Error(), "unexpected argument") {
				t.Fatalf("expected unexpected argument error for %q, got %v", trailing, err)
			}
		})
	}
}

func TestQuotaSetRejectsConflictingFlagsBeforeStoreLookup(t *testing.T) {
	t.Parallel()

	cmd := (&App{}).userCmd()
	cmd.SetArgs([]string{"quota-set", "--username", "alice", "--monthly-gb", "1", "--monthly-bytes", "1"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "only one") {
		t.Fatalf("expected quota flag conflict before store lookup, got %v", err)
	}
}

func TestUserCommandsRejectUnexpectedArgs(t *testing.T) {
	t.Parallel()

	cases := [][]string{
		{"add", "--username", "alice", "extra"},
		{"rm", "--username", "alice", "extra"},
		{"enable", "--username", "alice", "extra"},
		{"disable", "--username", "alice", "extra"},
		{"expiry-set", "--username", "alice", "--date", "2026-12-31", "extra"},
		{"expiry-clear", "--username", "alice", "extra"},
		{"reconcile", "--from-server", "main", "extra"},
		{"list", "--server", "main", "extra"},
		{"show", "--server", "main", "--username", "alice", "extra"},
		{"top", "--server", "main", "extra"},
		{"quota-reset", "--username", "alice", "extra"},
		{"link", "--server", "main", "--username", "alice", "extra"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args[:1], " "), func(t *testing.T) {
			cmd := (&App{}).userCmd()
			cmd.SetArgs(args)
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("unknown command %q", "extra")) && !strings.Contains(err.Error(), "accepts 0 arg(s)") {
				t.Fatalf("expected unexpected arg error for %v, got %v", args, err)
			}
		})
	}
}

func TestUserCommandsRejectBlankRequiredFlagsBeforeStoreLookup(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "add username", args: []string{"add", "--username", " "}, want: "--username is required"},
		{name: "rm username", args: []string{"rm", "--username", " "}, want: "--username is required"},
		{name: "enable username", args: []string{"enable", "--username", " "}, want: "--username is required"},
		{name: "disable username", args: []string{"disable", "--username", " "}, want: "--username is required"},
		{name: "expiry-set username", args: []string{"expiry-set", "--username", " ", "--date", "2026-12-31"}, want: "--username is required"},
		{name: "expiry-clear username", args: []string{"expiry-clear", "--username", " "}, want: "--username is required"},
		{name: "list server", args: []string{"list", "--server", " "}, want: "--server is required"},
		{name: "show server", args: []string{"show", "--server", " ", "--username", "alice"}, want: "--server is required"},
		{name: "show username", args: []string{"show", "--server", "main", "--username", " "}, want: "--username is required"},
		{name: "top server", args: []string{"top", "--server", " "}, want: "--server is required"},
		{name: "quota-reset username", args: []string{"quota-reset", "--username", " "}, want: "--username is required"},
		{name: "quota-set username", args: []string{"quota-set", "--username", " ", "--monthly-gb", "400"}, want: "--username is required"},
		{name: "link server", args: []string{"link", "--server", " ", "--username", "alice"}, want: "--server is required"},
		{name: "link username", args: []string{"link", "--server", "main", "--username", " "}, want: "--username is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := (&App{}).userCmd()
			cmd.SetArgs(tc.args)
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q for %v, got %v", tc.want, tc.args, err)
			}
		})
	}
}

func TestUserAddRejectsInvalidScalarInputsBeforeStoreLookup(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "negative quota", args: []string{"add", "--username", "alice", "--quota-bytes", "-1"}, want: "--quota-bytes must be >= 0"},
		{name: "bad uuid", args: []string{"add", "--username", "alice", "--uuid", "bad"}, want: "--uuid must be a valid UUID"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := (&App{}).userCmd()
			cmd.SetArgs(tc.args)
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q for %v, got %v", tc.want, tc.args, err)
			}
		})
	}
}
