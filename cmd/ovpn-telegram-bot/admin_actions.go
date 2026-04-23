package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (b *bot) adminActionsEnabled() bool {
	return strings.TrimSpace(b.adminToken) != "" && b.operator != nil
}

func (b *bot) adminDisabledReason() string {
	if strings.TrimSpace(b.adminToken) == "" {
		return "Admin actions are disabled: configure OVPN_TELEGRAM_ADMIN_TOKEN."
	}
	if b.operator == nil {
		return "Admin actions are disabled: Docker control socket is unavailable."
	}
	return "Admin actions are disabled."
}

func (b *bot) beginRestartConfirm(ctx context.Context, chatID int64, service string) error {
	if !b.adminActionsEnabled() {
		return b.sendPlainMessage(ctx, chatID, b.adminDisabledReason(), servicesInlineKeyboard(false, b.hasHAProxy()))
	}
	normalized, ok := normalizeServiceName(service)
	if !ok {
		return b.sendPlainMessage(ctx, chatID, "Unknown service. Allowed: "+restartableServicesHelp(b.hasHAProxy()), servicesInlineKeyboard(true, b.hasHAProxy()))
	}
	b.setConfirm(chatID, "restart", []string{normalized})
	text := fmt.Sprintf("Confirm restart for `%s`?\nTTL: %s", normalized, confirmTTL.String())
	return b.sendMarkdownMessage(ctx, chatID, text, confirmInlineKeyboard())
}

func (b *bot) beginHealConfirm(ctx context.Context, chatID int64) error {
	if !b.adminActionsEnabled() {
		return b.sendPlainMessage(ctx, chatID, b.adminDisabledReason(), servicesInlineKeyboard(false, b.hasHAProxy()))
	}
	b.setConfirm(chatID, "heal", nil)
	text := fmt.Sprintf("Confirm `/heal`?\nIt will restart only unhealthy restartable services.\nTTL: %s", confirmTTL.String())
	return b.sendMarkdownMessage(ctx, chatID, text, confirmInlineKeyboard())
}

func (b *bot) executeConfirm(ctx context.Context, chatID int64) error {
	st, ok := b.getConfirm(chatID)
	if !ok {
		return b.sendPlainMessage(ctx, chatID, "No pending action or confirmation expired.", servicesInlineKeyboard(b.adminActionsEnabled(), b.hasHAProxy()))
	}
	b.clearConfirm(chatID)

	switch st.Kind {
	case "restart":
		if len(st.Services) == 0 {
			return b.sendPlainMessage(ctx, chatID, "No services queued for restart.", servicesInlineKeyboard(b.adminActionsEnabled(), b.hasHAProxy()))
		}
		text := b.restartServicesWithVerification(ctx, st.Services)
		return b.sendPlainMessage(ctx, chatID, text, servicesInlineKeyboard(b.adminActionsEnabled(), b.hasHAProxy()))
	case "heal":
		text := b.healUnhealthyServices(ctx)
		return b.sendPlainMessage(ctx, chatID, text, servicesInlineKeyboard(b.adminActionsEnabled(), b.hasHAProxy()))
	default:
		return b.sendPlainMessage(ctx, chatID, "Unknown pending action. Use /services.", servicesInlineKeyboard(b.adminActionsEnabled(), b.hasHAProxy()))
	}
}

func (b *bot) healUnhealthyServices(ctx context.Context) string {
	before := b.collectAuditSnapshot(ctx)
	targets := before.unhealthyRestartables()
	if len(targets) == 0 {
		return strings.Join([]string{
			"Heal Result",
			"No unhealthy restartable services were found.",
			fmt.Sprintf("Overall: %s", before.Overall),
		}, "\n")
	}

	lines := []string{"Heal Result", "Targets: " + strings.Join(targets, ", "), ""}
	lines = append(lines, b.restartServicesWithVerification(ctx, targets))
	after := b.collectAuditSnapshot(ctx)
	healthy, total := after.serviceHealthCount()
	lines = append(lines,
		"",
		fmt.Sprintf("Post-heal overall: %s", after.Overall),
		fmt.Sprintf("Post-heal services: %d/%d healthy", healthy, total),
	)
	return strings.Join(lines, "\n")
}

func (b *bot) restartServicesWithVerification(ctx context.Context, services []string) string {
	restartSet := make(map[string]struct{}, len(services))
	normalized := make([]string, 0, len(services))
	for _, svc := range services {
		k, ok := normalizeServiceName(svc)
		if !ok {
			continue
		}
		if _, exists := restartSet[k]; exists {
			continue
		}
		restartSet[k] = struct{}{}
		normalized = append(normalized, k)
	}
	sort.SliceStable(normalized, func(i, j int) bool {
		return restartOrderWeight(normalized[i]) < restartOrderWeight(normalized[j])
	})

	if len(normalized) == 0 {
		return "Restart Result\nNo valid restart targets provided."
	}

	lines := []string{"Restart Result"}
	for _, svc := range normalized {
		restartCtx, cancelRestart := context.WithTimeout(ctx, 20*time.Second)
		err := b.operator.Restart(restartCtx, svc)
		cancelRestart()
		if err != nil {
			lines = append(lines, fmt.Sprintf("- %s: FAIL (restart error: %s)", serviceLabelForKey(svc), strings.TrimSpace(err.Error())))
			continue
		}
		verifyKey := svc
		if svc == "xray" {
			verifyKey = "xray-via-agent"
		}
		if verifyErr := b.waitServiceHealthy(ctx, verifyKey, 45*time.Second); verifyErr != nil {
			lines = append(lines, fmt.Sprintf("- %s: FAIL (verify error: %s)", serviceLabelForKey(svc), strings.TrimSpace(verifyErr.Error())))
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s: OK", serviceLabelForKey(svc)))
	}
	return strings.Join(lines, "\n")
}

func (b *bot) waitServiceHealthy(ctx context.Context, serviceKey string, timeout time.Duration) error {
	deadline := time.Now().UTC().Add(timeout)
	var lastDetail string
	for {
		snap := b.collectAuditSnapshot(ctx)
		svc, ok := findServiceCheck(snap.Services, serviceKey)
		if ok && svc.Healthy {
			return nil
		}
		if ok {
			lastDetail = svc.Detail
		} else {
			lastDetail = "service check not found"
		}
		if time.Now().UTC().After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	if strings.TrimSpace(lastDetail) == "" {
		lastDetail = "did not become healthy before timeout"
	}
	return errors.New(lastDetail)
}

func restartOrderWeight(service string) int {
	for i, item := range restartableServiceOrder {
		if item == service {
			return i
		}
	}
	return 100
}
