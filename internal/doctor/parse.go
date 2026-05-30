package doctor

import (
	"encoding/json"
	"strings"
)

type ServiceState struct {
	Service string `json:"service"`
	Name    string `json:"name,omitempty"`
	State   string `json:"state,omitempty"`
	Status  string `json:"status,omitempty"`
	Health  string `json:"health,omitempty"`
}

// ParseKV parses `key=value` lines into a map.
func ParseKV(raw string) map[string]string {
	out := map[string]string{}
	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		k := strings.TrimSpace(parts[0])
		v := strings.TrimSpace(parts[1])
		if k == "" {
			continue
		}
		out[k] = v
	}
	return out
}

// ParseComposePS parses the JSON output of `docker compose ps` into service states.
func ParseComposePS(raw string) ([]ServiceState, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if strings.HasPrefix(raw, "[") {
		var arr []map[string]any
		if err := json.Unmarshal([]byte(raw), &arr); err != nil {
			return nil, err
		}
		return mapToStates(arr), nil
	}

	lines := strings.Split(raw, "\n")
	arr := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, err
		}
		arr = append(arr, row)
	}
	return mapToStates(arr), nil
}

// mapToStates projects decoded compose-ps rows into ServiceState values.
func mapToStates(rows []map[string]any) []ServiceState {
	out := make([]ServiceState, 0, len(rows))
	for _, row := range rows {
		state := ServiceState{
			Service: strAny(row["Service"]),
			Name:    strAny(row["Name"]),
			State:   strAny(row["State"]),
			Status:  strAny(row["Status"]),
			Health:  strAny(row["Health"]),
		}
		if strings.TrimSpace(state.Service) == "" {
			state.Service = strings.TrimSpace(state.Name)
		}
		if state.Service == "" {
			continue
		}
		out = append(out, state)
	}
	return out
}

// strAny renders an arbitrary JSON value as a trimmed string.
func strAny(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}
