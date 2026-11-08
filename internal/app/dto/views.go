package dto

type Summary struct {
	State          string `json:"state"`
	Servers        int    `json:"servers"`
	EnabledServers int    `json:"enabledServers"`
	Users          int    `json:"users"`
	ReadOnly       bool   `json:"readOnly"`
}

// Server contains inventory only. Runtime health requires a separate observation.
type Server struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Role           string   `json:"role"`
	Enabled        bool     `json:"enabled"`
	PrimaryProfile string   `json:"primaryProfile"`
	Profiles       []string `json:"profiles"`
	XrayVersion    string   `json:"xrayVersion"`
	Runtime        string   `json:"runtime"`
}

type ServerPage struct {
	Items      []Server `json:"items"`
	NextCursor string   `json:"nextCursor,omitempty"`
}

type CheckRequest struct {
	ServerID string `json:"serverId"`
	Username string `json:"username"`
	Profile  string `json:"profile,omitempty"`
}

type Check struct {
	ServerID        string   `json:"serverId"`
	Profile         string   `json:"profile"`
	Source          string   `json:"source"`
	Kind            string   `json:"kind"`
	Outcome         string   `json:"outcome"`
	StatusCode      int      `json:"statusCode,omitempty"`
	DurationMS      int64    `json:"durationMs"`
	TTFBMS          int64    `json:"ttfbMs"`
	Bytes           int64    `json:"bytes"`
	ErrorCode       string   `json:"errorCode,omitempty"`
	MeasuredAt      string   `json:"measuredAt"`
	FreshForSeconds int      `json:"freshForSeconds"`
	Unverified      []string `json:"unverified"`
}
