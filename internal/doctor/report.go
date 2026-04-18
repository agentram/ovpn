package doctor

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Status string

const (
	StatusPass Status = "pass"
	StatusWarn Status = "warn"
	StatusFail Status = "fail"
	StatusSkip Status = "skip"
)

type Check struct {
	Name    string   `json:"name"`
	Status  Status   `json:"status"`
	Message string   `json:"message"`
	Details []string `json:"details,omitempty"`
	Hint    string   `json:"hint,omitempty"`
}

// Report is the aggregated output of `ovpn doctor`.
type Report struct {
	Server        string            `json:"server"`
	Timestamp     time.Time         `json:"timestamp"`
	OverallStatus Status            `json:"overall_status"`
	Checks        []Check           `json:"checks"`
	Logs          map[string]string `json:"logs,omitempty"`
}

// NewReport initializes report with the required dependencies.
func NewReport(server string) Report {
	return Report{
		Server:    server,
		Timestamp: time.Now().UTC(),
		Checks:    make([]Check, 0, 12),
	}
}

// Add applies add and returns an error on failure.
func (r *Report) Add(check Check) {
	if check.Status == "" {
		check.Status = StatusFail
	}
	r.Checks = append(r.Checks, check)
}

// Finalize returns finalize.
func (r *Report) Finalize() {
	r.OverallStatus = Summarize(r.Checks)
}

// JSON returns json.
func (r Report) JSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// Summarize returns summarize.
func Summarize(checks []Check) Status {
	overall := StatusSkip
	for _, c := range checks {
		switch c.Status {
		case StatusFail:
			return StatusFail
		case StatusWarn:
			overall = StatusWarn
		case StatusPass:
			if overall == StatusSkip {
				overall = StatusPass
			}
		}
	}
	return overall
}

// FormatHuman renders human into the format expected by callers.
func FormatHuman(r Report, verbose bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Server: %s\n", r.Server)
	fmt.Fprintf(&b, "Timestamp: %s\n", r.Timestamp.Format(time.RFC3339))
	fmt.Fprintf(&b, "Overall: %s\n\n", strings.ToUpper(string(r.OverallStatus)))
	for _, c := range r.Checks {
		fmt.Fprintf(&b, "[%s] %s", strings.ToUpper(string(c.Status)), c.Name)
		if strings.TrimSpace(c.Message) != "" {
			fmt.Fprintf(&b, ": %s", c.Message)
		}
		b.WriteString("\n")
		if verbose || c.Status != StatusPass {
			for _, d := range c.Details {
				d = strings.TrimSpace(d)
				if d == "" {
					continue
				}
				fmt.Fprintf(&b, "  - %s\n", d)
			}
		}
		if strings.TrimSpace(c.Hint) != "" && c.Status != StatusPass {
			fmt.Fprintf(&b, "  hint: %s\n", c.Hint)
		}
	}
	if len(r.Logs) > 0 {
		b.WriteString("\nRecent logs:\n")
		for svc, text := range r.Logs {
			fmt.Fprintf(&b, "--- %s ---\n", svc)
			b.WriteString(strings.TrimSpace(text))
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String()) + "\n"
}
