package main

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"ovpn/internal/telegrambot"
)

// pollLoop runs poll loop until context cancellation.
func (b *bot) pollLoop(ctx context.Context) {
	offset := int64(0)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		updates, err := b.tg.getUpdates(ctx, offset, 30)
		if err != nil {
			b.ensureHealth().onPollFailure(time.Now().UTC(), err)
			b.logger.Warn("telegram getUpdates failed", "error", err)
			time.Sleep(b.cfg.pollInterval)
			continue
		}
		b.ensureHealth().onPollSuccess(time.Now().UTC())
		for _, upd := range updates {
			if upd.UpdateID >= offset {
				offset = upd.UpdateID + 1
			}
			if upd.CallbackQuery != nil {
				b.handleCallback(ctx, upd.CallbackQuery)
				continue
			}
			if upd.Message != nil {
				b.handleMessage(ctx, upd.Message)
			}
		}
	}
}

// handleMessage executes message flow and returns the first error.
func (b *bot) handleMessage(ctx context.Context, msg *telegramMessage) {
	if msg == nil {
		return
	}
	userID := int64(0)
	if msg.From != nil {
		userID = msg.From.ID
	}
	if !b.isAllowed(msg.Chat.ID, userID) {
		return
	}
	b.ensureOwnerNotifyChat(msg.Chat.ID, userID)
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return
	}

	if isCancelCommand(text) {
		b.clearPrompt(msg.Chat.ID)
		b.clearConfirm(msg.Chat.ID)
		if err := b.sendPlainMessage(ctx, msg.Chat.ID, "Canceled.", mainReplyKeyboard()); err != nil {
			b.logger.Warn("telegram send cancel response failed", "chat_id", msg.Chat.ID, "error", err)
		}
		return
	}

	if st, ok := b.getPrompt(msg.Chat.ID); ok && b.isPromptInput(text) {
		if err := b.handlePromptInput(ctx, msg.Chat.ID, userID, st, text); err != nil {
			_ = b.sendPlainMessage(ctx, msg.Chat.ID, formatFriendlyError(err), mainReplyKeyboard())
		}
		return
	}

	if strings.HasPrefix(text, "/") {
		cmd, args, err := telegrambot.ParseCommand(text)
		if err != nil {
			return
		}
		if err := b.dispatchCommand(ctx, msg.Chat.ID, userID, cmd, args); err != nil {
			if sendErr := b.sendPlainMessage(ctx, msg.Chat.ID, formatFriendlyError(err), mainReplyKeyboard()); sendErr != nil {
				b.logger.Warn("telegram send command error failed", "chat_id", msg.Chat.ID, "command", cmd, "error", sendErr)
			}
		}
		return
	}

	action := menuActionFromText(text)
	if action == "" {
		_ = b.sendPlainMessage(ctx, msg.Chat.ID, "Use the menu buttons or /help.", mainReplyKeyboard())
		return
	}
	if err := b.dispatchMenuAction(ctx, msg.Chat.ID, userID, action); err != nil {
		_ = b.sendPlainMessage(ctx, msg.Chat.ID, formatFriendlyError(err), mainReplyKeyboard())
	}
}

// handleCallback executes callback flow and returns the first error.
func (b *bot) handleCallback(ctx context.Context, cb *telegramCallbackQuery) {
	if cb == nil || cb.Message == nil {
		return
	}
	chatID := cb.Message.Chat.ID
	userID := int64(0)
	if cb.From != nil {
		userID = cb.From.ID
	}
	if !b.isAllowed(chatID, userID) {
		_ = b.tg.answerCallbackQuery(ctx, cb.ID, "Not allowed")
		return
	}

	if err := b.dispatchCallback(ctx, chatID, userID, cb.Data); err != nil {
		_ = b.tg.answerCallbackQuery(ctx, cb.ID, err.Error())
		_ = b.sendPlainMessage(ctx, chatID, formatFriendlyError(err), mainReplyKeyboard())
		return
	}
	if err := b.tg.answerCallbackQuery(ctx, cb.ID, ""); err != nil {
		b.logger.Warn("telegram answerCallbackQuery failed", "chat_id", chatID, "error", err)
	}
}

// isAllowed enforces a strict owner-only read policy by default.
func (b *bot) isAllowed(_ int64, userID int64) bool {
	return b.isOwner(userID)
}

// ensureOwnerNotifyChat auto-registers owner chat for notifications when not configured.
func (b *bot) ensureOwnerNotifyChat(chatID int64, userID int64) {
	if !b.isOwner(userID) {
		return
	}
	if slices.Contains(b.notifyChats, chatID) {
		return
	}
	b.notifyChats = append(b.notifyChats, chatID)
	if b.logger != nil {
		b.logger.Info("owner chat added to notify targets", "chat_id", chatID)
	}
}

// formatFriendlyError renders short operator-friendly error text.
func formatFriendlyError(err error) string {
	if err == nil {
		return "Request failed."
	}
	return fmt.Sprintf("Request failed: %s", strings.TrimSpace(err.Error()))
}

// dispatchCommand returns dispatch command.
func (b *bot) dispatchCommand(ctx context.Context, chatID int64, _ int64, cmd string, args []string) error {
	switch cmd {
	case "/start", "/menu":
		return b.sendMainMenu(ctx, chatID)
	case "/help":
		return b.sendHelp(ctx, chatID)
	case "/guide":
		return b.sendGuide(ctx, chatID)
	case "/status":
		return b.sendPlainMessage(ctx, chatID, b.renderStatusSummary(ctx), mainReplyKeyboard())
	case "/services":
		return b.sendPlainMessage(ctx, chatID, b.renderServicesOverview(ctx), servicesInlineKeyboard(b.adminActionsEnabled()))
	case "/doctor":
		return b.sendPlainMessage(ctx, chatID, b.renderDoctorReport(ctx), servicesInlineKeyboard(b.adminActionsEnabled()))
	case "/users":
		messages, err := b.renderUsersList(ctx)
		if err != nil {
			return err
		}
		return b.sendHTMLChunks(ctx, chatID, messages, usersInlineKeyboard(true))
	case "/traffic":
		text, err := b.renderTrafficTotals(ctx)
		if err != nil {
			return err
		}
		return b.sendPlainMessage(ctx, chatID, text, trafficInlineKeyboard())
	case "/quota":
		text, err := b.renderQuotaSummary(ctx)
		if err != nil {
			return err
		}
		return b.sendPlainMessage(ctx, chatID, text, quotaInlineKeyboard())
	case "/restart":
		if len(args) == 0 {
			return b.sendPlainMessage(ctx, chatID, "Usage: /restart <service>\nAllowed: "+restartableServicesHelp(), servicesInlineKeyboard(b.adminActionsEnabled()))
		}
		return b.beginRestartConfirm(ctx, chatID, args[0])
	case "/heal":
		return b.beginHealConfirm(ctx, chatID)
	default:
		return b.sendHelp(ctx, chatID)
	}
}

// dispatchMenuAction returns dispatch menu action.
func (b *bot) dispatchMenuAction(ctx context.Context, chatID int64, _ int64, action string) error {
	switch action {
	case "home":
		return b.sendMainMenu(ctx, chatID)
	case "status":
		return b.sendPlainMessage(ctx, chatID, b.renderStatusSummary(ctx), mainReplyKeyboard())
	case "doctor":
		return b.sendPlainMessage(ctx, chatID, b.renderDoctorReport(ctx), servicesInlineKeyboard(b.adminActionsEnabled()))
	case "services":
		return b.sendPlainMessage(ctx, chatID, b.renderServicesOverview(ctx), servicesInlineKeyboard(b.adminActionsEnabled()))
	case "users":
		messages, err := b.renderUsersList(ctx)
		if err != nil {
			return err
		}
		return b.sendHTMLChunks(ctx, chatID, messages, usersInlineKeyboard(true))
	case "traffic":
		text, err := b.renderTrafficTotals(ctx)
		if err != nil {
			return err
		}
		return b.sendPlainMessage(ctx, chatID, text, trafficInlineKeyboard())
	case "quota":
		text, err := b.renderQuotaSummary(ctx)
		if err != nil {
			return err
		}
		return b.sendPlainMessage(ctx, chatID, text, quotaInlineKeyboard())
	case "help":
		return b.sendHelp(ctx, chatID)
	default:
		return b.sendMainMenu(ctx, chatID)
	}
}

// dispatchCallback returns dispatch callback.
func (b *bot) dispatchCallback(ctx context.Context, chatID int64, userID int64, data string) error {
	data = strings.TrimSpace(data)
	switch data {
	case "users:refresh":
		messages, err := b.renderUsersList(ctx)
		if err != nil {
			return err
		}
		return b.sendHTMLChunks(ctx, chatID, messages, usersInlineKeyboard(true))
	case "users:top":
		text, err := b.renderTopUsers(ctx, 10)
		if err != nil {
			return err
		}
		return b.sendPlainMessage(ctx, chatID, text, usersInlineKeyboard(true))
	case "users:link":
		if !b.isOwner(userID) {
			return b.sendPlainMessage(ctx, chatID, "Access denied: full user links are owner-only.", usersInlineKeyboard(false))
		}
		b.setPrompt(chatID, promptUserLink)
		return b.sendPlainMessage(ctx, chatID, "Send username or email to generate a user link. Use /cancel to stop.", usersInlineKeyboard(true))
	case "users:back":
		return b.sendMainMenu(ctx, chatID)
	case "traffic:totals":
		text, err := b.renderTrafficTotals(ctx)
		if err != nil {
			return err
		}
		return b.sendPlainMessage(ctx, chatID, text, trafficInlineKeyboard())
	case "traffic:top10":
		text, err := b.renderTopUsers(ctx, 10)
		if err != nil {
			return err
		}
		return b.sendPlainMessage(ctx, chatID, text, trafficInlineKeyboard())
	case "traffic:today":
		text, err := b.renderTrafficToday(ctx)
		if err != nil {
			return err
		}
		return b.sendPlainMessage(ctx, chatID, text, trafficInlineKeyboard())
	case "traffic:back":
		return b.sendMainMenu(ctx, chatID)
	case "quota:summary":
		text, err := b.renderQuotaSummary(ctx)
		if err != nil {
			return err
		}
		return b.sendPlainMessage(ctx, chatID, text, quotaInlineKeyboard())
	case "quota:over80":
		text, err := b.renderQuotaThreshold(ctx, 0.80, "Users over 80% quota")
		if err != nil {
			return err
		}
		return b.sendPlainMessage(ctx, chatID, text, quotaInlineKeyboard())
	case "quota:over95":
		text, err := b.renderQuotaThreshold(ctx, 0.95, "Users over 95% quota")
		if err != nil {
			return err
		}
		return b.sendPlainMessage(ctx, chatID, text, quotaInlineKeyboard())
	case "quota:blocked":
		text, err := b.renderQuotaBlocked(ctx)
		if err != nil {
			return err
		}
		return b.sendPlainMessage(ctx, chatID, text, quotaInlineKeyboard())
	case "quota:back":
		return b.sendMainMenu(ctx, chatID)
	case "services:overview":
		return b.sendPlainMessage(ctx, chatID, b.renderServicesOverview(ctx), servicesInlineKeyboard(b.adminActionsEnabled()))
	case "services:doctor":
		return b.sendPlainMessage(ctx, chatID, b.renderDoctorReport(ctx), servicesInlineKeyboard(b.adminActionsEnabled()))
	case "services:heal":
		return b.beginHealConfirm(ctx, chatID)
	case "services:back":
		return b.sendMainMenu(ctx, chatID)
	case "confirm:yes":
		return b.executeConfirm(ctx, chatID)
	case "confirm:no":
		b.clearConfirm(chatID)
		return b.sendPlainMessage(ctx, chatID, "Action canceled.", servicesInlineKeyboard(b.adminActionsEnabled()))
	}

	if strings.HasPrefix(data, "services:detail:") {
		key := strings.TrimSpace(strings.TrimPrefix(data, "services:detail:"))
		return b.sendPlainMessage(ctx, chatID, b.renderSingleService(ctx, key), servicesInlineKeyboard(b.adminActionsEnabled()))
	}
	if strings.HasPrefix(data, "services:restart:") {
		target := strings.TrimSpace(strings.TrimPrefix(data, "services:restart:"))
		return b.beginRestartConfirm(ctx, chatID, target)
	}
	return b.sendPlainMessage(ctx, chatID, "Unknown action. Use /menu.", mainReplyKeyboard())
}

// handlePromptInput executes prompt input flow and returns the first error.
func (b *bot) handlePromptInput(ctx context.Context, chatID int64, userID int64, st promptState, text string) error {
	defer b.clearPrompt(chatID)
	switch st.Kind {
	case promptUserLink:
		if !b.isOwner(userID) {
			return b.sendPlainMessage(ctx, chatID, "Access denied: full user links are owner-only.", mainReplyKeyboard())
		}
		link, err := b.buildUserLink(ctx, text)
		if err != nil {
			return b.sendPlainMessage(ctx, chatID, "Cannot build link: "+err.Error(), usersInlineKeyboard(true))
		}
		return b.sendPlainMessage(ctx, chatID, link, usersInlineKeyboard(true))
	default:
		return b.sendPlainMessage(ctx, chatID, "Prompt expired. Use /menu.", mainReplyKeyboard())
	}
}
