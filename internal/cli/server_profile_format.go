package cli

import (
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"

	"ovpn/internal/model"
)

func renderServerProfileTable(srv model.Server, enabled map[string]bool) string {
	tw := table.NewWriter()
	tw.SetStyle(table.StyleRounded)
	tw.SetColumnConfigs([]table.ColumnConfig{
		{Name: "Description", WidthMax: 58, WidthMaxEnforcer: text.WrapSoft},
	})
	tw.AppendHeader(table.Row{"Profile", "Status", "Port", "Enabled", "Primary", "Description"})
	primary := srv.NormalizedPrimaryProfile()
	for _, p := range model.SupportedTransportProfiles() {
		tw.AppendRow(table.Row{
			p.Name,
			p.Status,
			p.Port,
			yesDash(enabled[p.Name]),
			yesDash(primary == p.Name),
			p.Description,
		})
	}
	return tw.Render()
}

func yesDash(v bool) string {
	if v {
		return "yes"
	}
	return "-"
}
