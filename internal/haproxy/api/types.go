package api

type Backend struct {
	ServerID  string `json:"serverId"`
	Available *bool  `json:"available"`
}
type Status struct {
	Supported bool      `json:"supported"`
	State     string    `json:"state"`
	Code      string    `json:"code,omitempty"`
	CheckedAt string    `json:"checkedAt"`
	Backends  []Backend `json:"backends"`
}
