package cli

import (
	"github.com/jedib0t/go-pretty/v6/table"

	"ovpn/internal/model"
)

func renderTrafficTotalsTable(rows []model.UserTraffic) string {
	tw := table.NewWriter()
	tw.SetStyle(table.StyleRounded)
	tw.AppendHeader(table.Row{"Email", "Total", "Uplink", "Downlink"})
	for _, row := range rows {
		total := row.UplinkBytes + row.DownlinkBytes
		tw.AppendRow(table.Row{
			row.Email,
			formatTrafficBytes(total),
			formatTrafficBytes(row.UplinkBytes),
			formatTrafficBytes(row.DownlinkBytes),
		})
	}
	if len(rows) == 0 {
		tw.AppendRow(table.Row{"-", "-", "-", "-"})
	}
	return tw.Render()
}

func renderDailyTrafficTable(rows []model.UserTraffic, day string) string {
	tw := table.NewWriter()
	tw.SetStyle(table.StyleRounded)
	tw.AppendHeader(table.Row{"Email", "Date", "Total", "Uplink", "Downlink"})
	for _, row := range rows {
		total := row.UplinkBytes + row.DownlinkBytes
		tw.AppendRow(table.Row{
			row.Email,
			day,
			formatTrafficBytes(total),
			formatTrafficBytes(row.UplinkBytes),
			formatTrafficBytes(row.DownlinkBytes),
		})
	}
	if len(rows) == 0 {
		tw.AppendRow(table.Row{"-", day, "-", "-", "-"})
	}
	return tw.Render()
}
