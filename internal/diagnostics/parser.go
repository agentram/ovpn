package diagnostics

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"ovpn/internal/model"
)

var (
	tsPrefixRE = regexp.MustCompile(`^(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}(?:\.\d+)?)\s+(.+)$`)
	emailRE    = regexp.MustCompile(`\bemail:\s*([^\s]+)`)
)

// ParseAccessLine parses the stable parts of an Xray access log line.
func ParseAccessLine(line string) (model.ConnectionEvent, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return model.ConnectionEvent{}, false
	}
	m := tsPrefixRE.FindStringSubmatch(line)
	if len(m) != 3 {
		return model.ConnectionEvent{}, false
	}
	ts, err := parseAccessTimestamp(m[1])
	if err != nil {
		return model.ConnectionEvent{}, false
	}
	rest := strings.TrimSpace(m[2])
	fields := strings.Fields(rest)
	if len(fields) < 3 {
		return model.ConnectionEvent{}, false
	}
	source := fields[0]
	if strings.EqualFold(source, "from") && len(fields) > 1 {
		source = fields[1]
	}
	resultIdx := -1
	result := ""
	for i, field := range fields {
		switch strings.ToLower(strings.TrimSpace(field)) {
		case "accepted", "rejected":
			resultIdx = i
			result = strings.ToLower(field)
		}
		if resultIdx >= 0 {
			break
		}
	}
	if resultIdx <= 0 || resultIdx+1 >= len(fields) {
		return model.ConnectionEvent{}, false
	}
	email := parseEmail(line)
	if email == "" {
		return model.ConnectionEvent{}, false
	}
	dest, port, family := parseDestination(fields[resultIdx+1])
	return model.ConnectionEvent{
		Timestamp:          ts.UTC(),
		Result:             result,
		Email:              email,
		SourceNetwork:      MaskSourceNetwork(source),
		Destination:        dest,
		DestinationPort:    port,
		DestinationFamily:  family,
		RawDestinationHint: fields[resultIdx+1],
	}, true
}

func parseAccessTimestamp(raw string) (time.Time, error) {
	layout := "2006/01/02 15:04:05"
	if strings.Contains(raw, ".") {
		layout = "2006/01/02 15:04:05.999999999"
	}
	return time.ParseInLocation(layout, raw, time.UTC)
}

func parseEmail(line string) string {
	m := emailRE.FindStringSubmatch(line)
	if len(m) != 2 {
		return ""
	}
	return strings.Trim(strings.TrimSpace(m[1]), ",;")
}

func parseDestination(raw string) (host string, port int, family string) {
	family = "unknown"
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", 0, family
	}
	if idx := strings.Index(raw, ":"); idx >= 0 {
		prefix := strings.ToLower(raw[:idx])
		if prefix == "tcp" || prefix == "udp" {
			raw = raw[idx+1:]
		}
	}
	raw = strings.Trim(raw, " ")
	host, port = splitHostPortLoose(raw)
	host = strings.Trim(host, "[]")
	if host == "" {
		return "", port, family
	}
	ip := net.ParseIP(host)
	switch {
	case ip == nil:
		family = "domain"
	case ip.To4() != nil:
		family = "ipv4"
	default:
		family = "ipv6"
	}
	return host, port, family
}

func splitHostPortLoose(raw string) (string, int) {
	if h, p, err := net.SplitHostPort(raw); err == nil {
		port, _ := strconv.Atoi(p)
		return h, port
	}
	if strings.HasPrefix(raw, "[") {
		if end := strings.LastIndex(raw, "]"); end > 0 {
			host := raw[1:end]
			rest := strings.TrimPrefix(raw[end+1:], ":")
			port, _ := strconv.Atoi(rest)
			return host, port
		}
	}
	idx := strings.LastIndex(raw, ":")
	if idx <= 0 {
		return raw, 0
	}
	host := raw[:idx]
	port, err := strconv.Atoi(raw[idx+1:])
	if err != nil {
		return raw, 0
	}
	return host, port
}

// MaskSourceNetwork returns a privacy-preserving source network bucket.
func MaskSourceNetwork(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return ""
	}
	host := source
	if h, _, err := net.SplitHostPort(source); err == nil {
		host = h
	} else if strings.HasPrefix(source, "[") {
		if end := strings.LastIndex(source, "]"); end > 0 {
			host = source[1:end]
		}
	} else if idx := strings.LastIndex(source, ":"); idx > 0 && strings.Count(source, ":") == 1 {
		host = source[:idx]
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil {
		return ""
	}
	if v4 := ip.To4(); v4 != nil {
		return fmt.Sprintf("%d.%d.%d.0/24", v4[0], v4[1], v4[2])
	}
	masked := ip.Mask(net.CIDRMask(56, 128))
	return masked.String() + "/56"
}
