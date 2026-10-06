package defaults

import "strings"

const (
	DefaultXrayVersion       = "26.7.28"
	DefaultXrayImageRepo     = "ghcr.io/xtls/xray-core"
	DefaultAgentImage        = "alpine:3.24.2"
	DefaultTelegramBotImage  = "alpine:3.24.2"
	DefaultWebImage          = "nginx:1.30.5-alpine"
	DefaultHAProxyImage      = "haproxy:3.2.25-alpine3.24"
	DefaultPrometheusImage   = "prom/prometheus:v3.15.0"
	DefaultAlertmanagerImage = "prom/alertmanager:v0.34.1"
	DefaultGrafanaImage      = "grafana/grafana:12.4.12"
	DefaultNodeExporterImage = "prom/node-exporter:v1.12.1"
	DefaultCAdvisorImage     = "ghcr.io/google/cadvisor:0.60.6"
)

func DefaultXrayImage(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		version = DefaultXrayVersion
	}
	return DefaultXrayImageRepo + ":" + version
}
