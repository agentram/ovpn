package cli

import (
	"github.com/jedib0t/go-pretty/v6/table"

	"ovpn/internal/model"
)

func renderServerListTable(servers []model.Server) string {
	tw := table.NewWriter()
	tw.SetStyle(table.StyleRounded)
	tw.AppendHeader(table.Row{"ID", "Name", "Role", "Host", "Domain", "Xray", "State"})
	for _, s := range servers {
		state := "disabled"
		if s.Enabled {
			state = "enabled"
		}
		tw.AppendRow(table.Row{s.ID, s.Name, s.NormalizedRole(), s.Host, s.Domain, s.XrayVersion, state})
	}
	return tw.Render()
}
