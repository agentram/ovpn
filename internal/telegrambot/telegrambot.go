package telegrambot

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var containerHostPattern = regexp.MustCompile(`^[a-f0-9]{12,}$`)

type AlertmanagerWebhook struct {
	Status            string            `json:"status"`
	Receiver          string            `json:"receiver"`
	GroupLabels       map[string]string `json:"groupLabels"`
	CommonLabels      map[string]string `json:"commonLabels"`
	CommonAnnotations map[string]string `json:"commonAnnotations"`
	ExternalURL       string            `json:"externalURL"`
	Alerts            []Alert           `json:"alerts"`
}

type Alert struct {
	Status       string            `json:"status"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
}

type NotifyEvent struct {
	Event    string `json:"event"`
	Status   string `json:"status"`
	Severity string `json:"severity"`
	Source   string `json:"source"`
	Message  string `json:"message"`
}

// ParseIDSetCSV parses id set csv and returns normalized values.
func ParseIDSetCSV(raw string) (map[int64]struct{}, error) {
	out := map[int64]struct{}{}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return out, nil
	}
	items := strings.Split(raw, ",")
	for _, item := range items {
		v := strings.TrimSpace(item)
		if v == "" {
			continue
		}
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid id %q: %w", v, err)
		}
		out[id] = struct{}{}
	}
	return out, nil
}

// ParseIDSliceCSV parses id slice csv and returns normalized values.
func ParseIDSliceCSV(raw string) ([]int64, error) {
	set, err := ParseIDSetCSV(raw)
	if err != nil {
		return nil, err
	}
	out := make([]int64, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

// IsAllowed reports whether is allowed is true.
func IsAllowed(chatID int64, userID int64, allowedChatIDs map[int64]struct{}, allowedUserIDs map[int64]struct{}) bool {
	if len(allowedChatIDs) == 0 && len(allowedUserIDs) == 0 {
		return false
	}
	if _, ok := allowedChatIDs[chatID]; ok {
		return true
	}
	if _, ok := allowedUserIDs[userID]; ok {
		return true
	}
	return false
}

// ParseCommand parses command and returns normalized values.
func ParseCommand(text string) (string, []string, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", nil, errors.New("empty command")
	}
	fields := strings.Fields(trimmed)
	cmd := fields[0]
	if !strings.HasPrefix(cmd, "/") {
		return "", nil, errors.New("not a telegram command")
	}
	cmd = strings.ToLower(strings.TrimSpace(strings.Split(cmd, "@")[0]))
	return cmd, fields[1:], nil
}

// RenderHelp renders help into the format expected by callers.
func RenderHelp() string {
	return strings.Join([]string{
		"ovpn telegram commands:",
		"/start, /menu - open main menu",
		"/help - show this message",
		"/status - compact status summary",
		"/services - services overview",
		"/doctor - detailed diagnostics",
		"/users - users quota audit (users mirrored across servers)",
		"/traffic - traffic totals summary (local server)",
		"/quota - rolling 30d quota summary (local server)",
		"/restart <service> - owner restart with confirmation",
		"/heal - owner auto-heal unhealthy services",
		"/guide - send VPN client PDF guide",
		"/cancel - cancel active prompt",
	}, "\n")
}

// RenderNotifyMessage renders notify message into the format expected by callers.
func RenderNotifyMessage(ev NotifyEvent) string {
	event := defaultText(ev.Event, "event")
	status := strings.ToUpper(defaultText(ev.Status, "info"))
	source := defaultText(ev.Source, "ovpn")
	severity := defaultText(ev.Severity, "info")
	msg := strings.TrimSpace(ev.Message)
	lines := []string{
		fmt.Sprintf("[%s] %s", status, event),
		fmt.Sprintf("source: %s | severity: %s", source, severity),
	}
	if msg != "" {
		lines = append(lines, "message: "+msg)
	}
	return strings.Join(lines, "\n")
}

// RenderAlertmanagerMessage renders alertmanager message into the format expected by callers.
func RenderAlertmanagerMessage(in AlertmanagerWebhook) string {
	status := strings.ToUpper(defaultText(in.Status, "firing"))
	name := firstNonEmpty(in.CommonLabels["alertname"], in.GroupLabels["alertname"], "alert")
	if text, ok := renderFriendlyAlertmanagerMessage(name, status, in); ok {
		return text
	}
	severity := defaultText(in.CommonLabels["severity"], "unknown")
	count := len(in.Alerts)
	summary := defaultText(in.CommonAnnotations["summary"], "")
	description := defaultText(in.CommonAnnotations["description"], "")
	lines := []string{
		fmt.Sprintf("[ALERT %s] %s", status, name),
		fmt.Sprintf("severity: %s | alerts: %d", severity, count),
	}
	if src := alertSource(in); src != "" {
		lines = append(lines, "source: "+src)
	}
	if summary != "" {
		lines = append(lines, "summary: "+summary)
	}
	if description != "" {
		lines = append(lines, "details: "+description)
	}
	if in.ExternalURL != "" {
		lines = append(lines, "alertmanager: "+renderAlertmanagerURL(in.ExternalURL))
	}
	return strings.Join(lines, "\n")
}

func renderFriendlyAlertmanagerMessage(name, status string, in AlertmanagerWebhook) (string, bool) {
	switch name {
	case "OVPNUserExpirySoon":
		email := firstNonEmpty(in.CommonLabels["email"], firstAlertLabel(in, "email"))
		expiryDate := firstNonEmpty(in.CommonLabels["expiry_date"], firstAlertLabel(in, "expiry_date"))
		username, server := splitUserIdentity(email)
		header := "User Expiry Reminder"
		if status != "FIRING" {
			header = "User Expiry Update"
		}
		lines := []string{header}
		if username != "" {
			lines = append(lines, "User: "+username)
		}
		if server != "" {
			lines = append(lines, "Server: "+server)
		}
		if email != "" {
			lines = append(lines, "Identity: "+email)
		}
		if expiryDate != "" {
			lines = append(lines, "Expires on: "+expiryDate+" UTC")
			lines = append(lines, "Access remains active until the end of that day.")
		}
		lines = append(lines, "Action: extend the expiry date if this user should keep access.")
		return strings.Join(lines, "\n"), true
	default:
		return "", false
	}
}

func firstAlertLabel(in AlertmanagerWebhook, key string) string {
	if len(in.Alerts) == 0 {
		return ""
	}
	return strings.TrimSpace(in.Alerts[0].Labels[key])
}

func splitUserIdentity(email string) (string, string) {
	email = strings.TrimSpace(email)
	if email == "" {
		return "", ""
	}
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return email, ""
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

// alertSource builds compact source details from first alert labels.
func alertSource(in AlertmanagerWebhook) string {
	if len(in.Alerts) == 0 {
		return ""
	}
	labels := in.Alerts[0].Labels
	instance := strings.TrimSpace(labels["instance"])
	job := strings.TrimSpace(labels["job"])
	switch {
	case instance != "" && job != "":
		return job + " (" + instance + ")"
	case instance != "":
		return instance
	case job != "":
		return job
	default:
		return ""
	}
}

// renderAlertmanagerURL returns normalized alertmanager URL for operator-facing text.
func renderAlertmanagerURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil {
		return strings.TrimSpace(raw)
	}
	host := strings.TrimSpace(u.Hostname())
	if host == "" {
		return strings.TrimSpace(raw)
	}
	// Alertmanager without explicit --web.external-url may emit container-id hostname.
	// Replace it with stable service name to avoid confusing links in Telegram messages.
	if containerHostPattern.MatchString(host) {
		if port := strings.TrimSpace(u.Port()); port != "" {
			u.Host = "alertmanager:" + port
		} else {
			u.Host = "alertmanager"
		}
	}
	return u.String()
}

// defaultText normalizes text and applies fallback defaults.
func defaultText(v, fallback string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return fallback
	}
	return v
}

// firstNonEmpty normalizes non empty and applies fallback defaults.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
