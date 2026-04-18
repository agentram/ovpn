package util

import (
	"errors"
	"strings"
	"testing"
)

func TestParseCSV(t *testing.T) {
	t.Parallel()

	got := ParseCSV(" a, b ,, c ")
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("unexpected ParseCSV output: %#v", got)
	}
}

func TestJoinCSV(t *testing.T) {
	t.Parallel()

	if got := JoinCSV([]string{"a", "b", "c"}); got != "a,b,c" {
		t.Fatalf("unexpected JoinCSV output: %q", got)
	}
}

func TestRequireNonEmpty(t *testing.T) {
	t.Parallel()

	if err := RequireNonEmpty("name", " "); err == nil {
		t.Fatalf("expected error for blank input")
	}
	if err := RequireNonEmpty("name", "value"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCombineErrors(t *testing.T) {
	t.Parallel()

	err := CombineErrors(nil, errors.New("a"), nil, errors.New("b"))
	if err == nil {
		t.Fatalf("expected combined error")
	}
	if !strings.Contains(err.Error(), "a") || !strings.Contains(err.Error(), "b") {
		t.Fatalf("unexpected combined error: %q", err.Error())
	}
}
