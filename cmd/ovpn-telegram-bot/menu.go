package main

import "strings"

// menuActionFromText returns menu action from text.
func menuActionFromText(text string) string {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case strings.ToLower(menuHome):
		return "home"
	case strings.ToLower(menuStatus):
		return "status"
	case strings.ToLower(menuDoctor):
		return "doctor"
	case strings.ToLower(menuServices):
		return "services"
	case strings.ToLower(menuUsers):
		return "users"
	case strings.ToLower(menuTraffic):
		return "traffic"
	case strings.ToLower(menuQuota):
		return "quota"
	case strings.ToLower(menuHelp):
		return "help"
	default:
		return ""
	}
}

// mainReplyKeyboard builds the keyboard layout for main reply actions.
func mainReplyKeyboard() map[string]any {
	return map[string]any{
		"keyboard": [][]map[string]string{
			{{"text": menuHome}, {"text": menuStatus}},
			{{"text": menuDoctor}, {"text": menuServices}},
			{{"text": menuUsers}, {"text": menuTraffic}},
			{{"text": menuQuota}, {"text": menuHelp}},
		},
		"resize_keyboard":   true,
		"is_persistent":     true,
		"one_time_keyboard": false,
	}
}

// usersInlineKeyboard builds the keyboard layout for users inline actions.
func usersInlineKeyboard(owner bool) map[string]any {
	rows := [][]map[string]string{
		{{"text": "Refresh", "callback_data": "users:refresh"}, {"text": "Top Traffic", "callback_data": "users:top"}},
	}
	if owner {
		rows = append(rows, []map[string]string{{"text": "User link", "callback_data": "users:link"}})
	}
	rows = append(rows, []map[string]string{{"text": "Back", "callback_data": "users:back"}})
	return map[string]any{"inline_keyboard": rows}
}

// trafficInlineKeyboard builds the keyboard layout for traffic inline actions.
func trafficInlineKeyboard() map[string]any {
	return map[string]any{"inline_keyboard": [][]map[string]string{
		{{"text": "Totals", "callback_data": "traffic:totals"}, {"text": "Top 10", "callback_data": "traffic:top10"}},
		{{"text": "Today", "callback_data": "traffic:today"}},
		{{"text": "Back", "callback_data": "traffic:back"}},
	}}
}

// quotaInlineKeyboard builds the keyboard layout for quota inline actions.
func quotaInlineKeyboard() map[string]any {
	return map[string]any{"inline_keyboard": [][]map[string]string{
		{{"text": "Summary", "callback_data": "quota:summary"}},
		{{"text": "Over 80%", "callback_data": "quota:over80"}, {"text": "Over 95%", "callback_data": "quota:over95"}},
		{{"text": "Blocked", "callback_data": "quota:blocked"}},
		{{"text": "Back", "callback_data": "quota:back"}},
	}}
}

// servicesInlineKeyboard builds the keyboard layout for services actions.
func servicesInlineKeyboard(adminEnabled bool, includeHAProxy bool) map[string]any {
	rows := [][]map[string]string{
		{{"text": "Overview", "callback_data": "services:overview"}, {"text": "Doctor", "callback_data": "services:doctor"}},
		{{"text": "Agent", "callback_data": "services:detail:ovpn-agent"}, {"text": "Xray", "callback_data": "services:detail:xray-via-agent"}},
		{{"text": "Prometheus", "callback_data": "services:detail:prometheus"}, {"text": "Alertmanager", "callback_data": "services:detail:alertmanager"}},
		{{"text": "Grafana", "callback_data": "services:detail:grafana"}, {"text": "Node Exporter", "callback_data": "services:detail:node-exporter"}},
		{{"text": "cAdvisor", "callback_data": "services:detail:cadvisor"}, {"text": "Bot Self", "callback_data": "services:detail:ovpn-telegram-bot"}},
	}
	if includeHAProxy {
		rows = append(rows, []map[string]string{{"text": "HAProxy", "callback_data": "services:detail:haproxy"}})
	}
	if adminEnabled {
		rows = append(rows,
			[]map[string]string{{"text": "Heal Unhealthy", "callback_data": "services:heal"}},
			[]map[string]string{{"text": "Restart Xray", "callback_data": "services:restart:xray"}, {"text": "Restart Agent", "callback_data": "services:restart:ovpn-agent"}},
			[]map[string]string{{"text": "Restart Prom", "callback_data": "services:restart:prometheus"}, {"text": "Restart Alert", "callback_data": "services:restart:alertmanager"}},
			[]map[string]string{{"text": "Restart Grafana", "callback_data": "services:restart:grafana"}, {"text": "Restart Node", "callback_data": "services:restart:node-exporter"}},
			[]map[string]string{{"text": "Restart cAdvisor", "callback_data": "services:restart:cadvisor"}},
		)
		if includeHAProxy {
			rows = append(rows, []map[string]string{{"text": "Restart HAProxy", "callback_data": "services:restart:haproxy"}})
		}
	}
	rows = append(rows, []map[string]string{{"text": "Back", "callback_data": "services:back"}})
	return map[string]any{"inline_keyboard": rows}
}

// confirmInlineKeyboard builds confirmation keyboard for mutating owner actions.
func confirmInlineKeyboard() map[string]any {
	return map[string]any{"inline_keyboard": [][]map[string]string{
		{{"text": "Confirm", "callback_data": "confirm:yes"}, {"text": "Cancel", "callback_data": "confirm:no"}},
	}}
}
