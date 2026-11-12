// Package haproxy projects local HAProxy health without exposing metric labels.
package haproxy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"ovpn/internal/haproxy/api"

	"github.com/prometheus/common/expfmt"
	prommodel "github.com/prometheus/common/model"
)

type Backend = api.Backend
type Status = api.Status

func Unknown(code string) Status {
	return Status{State: "unknown", Code: code, CheckedAt: time.Now().UTC().Format(time.RFC3339), Backends: []Backend{}}
}

// Read uses the configured container endpoint, without environment proxies or redirects.
func Read(ctx context.Context, endpoint string) Status {
	if endpoint == "" {
		return Unknown("HA_NOT_CONFIGURED")
	}
	result := Unknown("HA_METRICS_UNAVAILABLE")
	result.Supported = true
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return result
	}
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		return result
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return result
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 {
		return result
	}
	parsed, err := Parse(strings.NewReader(string(body)))
	if err != nil {
		result.Code = "HA_METRICS_INVALID"
		return result
	}
	return parsed
}

func Parse(reader io.Reader) (Status, error) {
	result := Unknown("HA_CONFIG_UPDATE_REQUIRED")
	result.Supported = true
	parser := expfmt.NewTextParser(prommodel.UTF8Validation)
	families, err := parser.TextToMetricFamilies(reader)
	if err != nil {
		return result, errors.New("invalid HA metrics")
	}
	family := families["haproxy_server_status"]
	if family == nil {
		return result, errors.New("missing HA metrics")
	}
	selected := map[string]bool{}
	for _, metric := range family.Metric {
		labels := map[string]string{}
		for _, pair := range metric.Label {
			labels[pair.GetName()] = pair.GetValue()
		}
		if labels["proxy"] != "foreign_backends" || metric.GetGauge().GetValue() != 1 {
			continue
		}
		node := strings.TrimPrefix(labels["server"], "ovpn_node_")
		id, e := strconv.ParseInt(node, 10, 64)
		if e != nil || id <= 0 || labels["server"] != "ovpn_node_"+strconv.FormatInt(id, 10) {
			continue
		}
		if !strings.Contains("|UP|DOWN|MAINT|DRAIN|NOLB|", "|"+labels["state"]+"|") {
			continue
		}
		key := "node-" + node
		if _, duplicate := selected[key]; duplicate {
			return result, errors.New("ambiguous HA metric state")
		}
		selected[key] = labels["state"] == "UP"
	}
	if len(selected) == 0 {
		return result, nil
	}
	up := 0
	for id, available := range selected {
		value := available
		result.Backends = append(result.Backends, Backend{ServerID: id, Available: &value})
		if available {
			up++
		}
	}
	sort.Slice(result.Backends, func(i, j int) bool { return result.Backends[i].ServerID < result.Backends[j].ServerID })
	result.Code = ""
	result.State = "online"
	if up == 0 {
		result.State = "failed"
	} else if up < len(selected) {
		result.State = "degraded"
	}
	return result, nil
}
