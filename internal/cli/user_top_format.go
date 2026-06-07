package cli

import (
	"fmt"

	"github.com/jedib0t/go-pretty/v6/table"
)

func renderUserTopTable(rows []userTopRow) string {
	tw := table.NewWriter()
	tw.SetStyle(table.StyleRounded)
	tw.AppendHeader(table.Row{"Rank", "User", "Email", "Total", "Uplink", "Downlink", "Quota %", "Blocked"})
	for _, row := range rows {
		quotaPct := "-"
		if row.QuotaPercent != nil {
			quotaPct = formatPercent(*row.QuotaPercent)
		}
		tw.AppendRow(table.Row{
			row.Rank,
			row.Username,
			row.Email,
			formatTrafficBytes(row.TotalBytes),
			formatTrafficBytes(row.UplinkBytes),
			formatTrafficBytes(row.DownlinkBytes),
			quotaPct,
			yesNo(row.BlockedByQuota),
		})
	}
	if len(rows) == 0 {
		tw.AppendRow(table.Row{"-", "-", "-", "-", "-", "-", "-", "-"})
	}
	return tw.Render()
}

func formatPercent(v float64) string {
	return fmt.Sprintf("%.1f%%", v)
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
