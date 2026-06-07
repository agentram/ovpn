package cli

import (
	"fmt"

	"github.com/jedib0t/go-pretty/v6/table"

	"ovpn/internal/model"
)

func renderProxyBackendTable(mappings []model.ProxyBackend) string {
	tw := table.NewWriter()
	tw.SetStyle(table.StyleRounded)
	tw.AppendHeader(table.Row{"Backend", "Priority", "State"})
	for _, mapping := range mappings {
		name := fmt.Sprintf("%d", mapping.BackendServerID)
		if mapping.BackendServer != nil {
			name = mapping.BackendServer.Name
		}
		state := "disabled"
		if mapping.Enabled {
			state = "enabled"
		}
		tw.AppendRow(table.Row{name, mapping.Priority, state})
	}
	if len(mappings) == 0 {
		tw.AppendRow(table.Row{"-", "-", "-"})
	}
	return tw.Render()
}
