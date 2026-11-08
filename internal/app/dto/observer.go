package dto

type ObservationTarget struct {
	ServerID string `json:"serverId"`
	Username string `json:"username"`
	Profile  string `json:"profile"`
}

type ObservationPolicy struct {
	ID               string              `json:"id"`
	Name             string              `json:"name"`
	Engine           string              `json:"engine"`
	Targets          []ObservationTarget `json:"targets"`
	IntervalSeconds  int                 `json:"intervalSeconds"`
	MaxChecksPerDay  int                 `json:"maxChecksPerDay"`
	Enabled          bool                `json:"enabled"`
	TrafficConfirmed bool                `json:"trafficConfirmed"`
}

type Observation struct {
	JobID           string            `json:"jobId"`
	ID              int64             `json:"id"`
	Target          ObservationTarget `json:"target"`
	Source          string            `json:"source"`
	EngineVersion   string            `json:"engineVersion"`
	Revision        string            `json:"revision"`
	Outcome         string            `json:"outcome"`
	FailureClass    string            `json:"failureClass,omitempty"`
	Code            string            `json:"code,omitempty"`
	StatusCode      int               `json:"statusCode,omitempty"`
	DurationMS      int64             `json:"durationMs"`
	TTFBMS          int64             `json:"ttfbMs"`
	Bytes           int64             `json:"bytes"`
	MeasuredAt      string            `json:"measuredAt"`
	SessionID       string            `json:"sessionId"`
	Fresh           bool              `json:"fresh"`
	FreshForSeconds int               `json:"freshForSeconds"`
	Unverified      []string          `json:"unverified"`
}

type ObservationIncident struct {
	Revision  string            `json:"revision"`
	Target    ObservationTarget `json:"target"`
	Source    string            `json:"source"`
	Class     string            `json:"class"`
	State     string            `json:"state"`
	Failures  int               `json:"failures"`
	Successes int               `json:"successes"`
	FirstSeen string            `json:"firstSeen"`
	LastSeen  string            `json:"lastSeen"`
}

type ObservationJob struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Engine     string `json:"engine"`
	PolicyID   string `json:"policyId,omitempty"`
	State      string `json:"state"`
	Code       string `json:"code,omitempty"`
	Total      int    `json:"total"`
	Completed  int    `json:"completed"`
	StartedAt  string `json:"startedAt"`
	FinishedAt string `json:"finishedAt,omitempty"`
}

type ObservationTool struct {
	Engine     string `json:"engine"`
	Version    string `json:"version"`
	Configured bool   `json:"configured"`
}

type ObserverSnapshot struct {
	Running           bool                  `json:"running"`
	Owner             string                `json:"owner"`
	SessionID         string                `json:"sessionId"`
	Heartbeat         string                `json:"heartbeat,omitempty"`
	PreviousHeartbeat string                `json:"previousHeartbeat,omitempty"`
	Policies          []ObservationPolicy   `json:"policies"`
	Tools             []ObservationTool     `json:"tools"`
	Jobs              []ObservationJob      `json:"jobs"`
	Latest            []Observation         `json:"latest"`
	History           []Observation         `json:"history"`
	Incidents         []ObservationIncident `json:"incidents"`
	RetainedSamples   int                   `json:"retainedSamples"`
	HistoryLimit      int                   `json:"historyLimit"`
}

type DiagnosticRequest struct {
	Engine           string              `json:"engine"`
	Targets          []ObservationTarget `json:"targets"`
	TrafficConfirmed bool                `json:"trafficConfirmed"`
}

type ToolSetup struct {
	Engine string `json:"engine"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type PolicyRunRequest struct {
	PolicyID         string `json:"policyId"`
	TrafficConfirmed bool   `json:"trafficConfirmed"`
}
