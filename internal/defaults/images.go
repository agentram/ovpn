package defaults

import "strings"

const (
	DefaultXrayVersion       = "26.3.27"
	DefaultXrayImageRepo     = "ghcr.io/xtls/xray-core"
	DefaultAgentImage        = "alpine:3.23.4"
	DefaultTelegramBotImage  = "alpine:3.23.4"
	DefaultPrometheusImage   = "prom/prometheus:v3.11.2"
	DefaultAlertmanagerImage = "prom/alertmanager:v0.32.0"
	DefaultGrafanaImage      = "grafana/grafana:12.4.3"
	DefaultNodeExporterImage = "prom/node-exporter:v1.11.1"
	DefaultCAdvisorImage     = "ghcr.io/google/cadvisor:0.56.2"
)

func DefaultXrayImage(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		version = DefaultXrayVersion
	}
	return DefaultXrayImageRepo + ":" + version
}
