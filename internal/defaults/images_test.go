package defaults

import (
	"os"
	"strings"
	"testing"
)

func TestDefaultsMatchExampleFiles(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		path string
		want []string
	}{
		{
			path: "../../.env.example",
			want: []string{
				"XRAY_IMAGE=" + DefaultXrayImage(DefaultXrayVersion),
				"OVPN_AGENT_IMAGE=" + DefaultAgentImage,
				"OVPN_TELEGRAM_BOT_IMAGE=" + DefaultTelegramBotImage,
				"PROMETHEUS_IMAGE=" + DefaultPrometheusImage,
				"ALERTMANAGER_IMAGE=" + DefaultAlertmanagerImage,
				"GRAFANA_IMAGE=" + DefaultGrafanaImage,
				"NODE_EXPORTER_IMAGE=" + DefaultNodeExporterImage,
				"CADVISOR_IMAGE=" + DefaultCAdvisorImage,
			},
		},
		{
			path: "../../examples/.env.example",
			want: []string{
				"XRAY_IMAGE=" + DefaultXrayImage(DefaultXrayVersion),
				"OVPN_AGENT_IMAGE=" + DefaultAgentImage,
				"OVPN_TELEGRAM_BOT_IMAGE=" + DefaultTelegramBotImage,
				"PROMETHEUS_IMAGE=" + DefaultPrometheusImage,
				"ALERTMANAGER_IMAGE=" + DefaultAlertmanagerImage,
				"GRAFANA_IMAGE=" + DefaultGrafanaImage,
				"NODE_EXPORTER_IMAGE=" + DefaultNodeExporterImage,
				"CADVISOR_IMAGE=" + DefaultCAdvisorImage,
			},
		},
		{
			path: "../../ansible/inventories/example/group_vars/all.yml",
			want: []string{
				`ovpn_agent_image: "` + DefaultAgentImage + `"`,
				`ovpn_xray_version: "` + DefaultXrayVersion + `"`,
			},
		},
	} {
		tc := tc
		t.Run(tc.path, func(t *testing.T) {
			raw, err := os.ReadFile(tc.path)
			if err != nil {
				t.Fatalf("read %s: %v", tc.path, err)
			}
			text := string(raw)
			for _, want := range tc.want {
				if !strings.Contains(text, want) {
					t.Fatalf("%s missing %q", tc.path, want)
				}
			}
		})
	}
}
