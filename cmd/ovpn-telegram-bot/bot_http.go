package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"ovpn/internal/telegrambot"
)

// handleHealth executes health flow and returns the first error.
func (b *bot) handleHealth(w http.ResponseWriter, _ *http.Request) {
	health := b.healthSnapshot()
	statusCode := http.StatusOK
	if !health.OK {
		statusCode = http.StatusServiceUnavailable
	}
	writeJSON(w, statusCode, map[string]any{
		"ok":                health.OK,
		"status":            health.Status,
		"service":           "ovpn-telegram-bot",
		"notify_chat_count": len(b.notifyChats),
		"owner_user_id":     b.cfg.ownerUserID,
		"access_model":      "owner-only",
		"link_feature":      b.linkFeatureStatus(),
		"health":            health,
		"time":              time.Now().UTC().Format(time.RFC3339),
	})
}

// handleAlertmanagerWebhook executes alertmanager webhook flow and returns the first error.
func (b *bot) handleAlertmanagerWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	var payload telegrambot.AlertmanagerWebhook
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid payload"})
		return
	}
	text := telegrambot.RenderAlertmanagerMessage(payload)
	if err := b.sendToNotifyChats(r.Context(), text); err != nil {
		b.logger.Warn("send alertmanager telegram message failed", "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "telegram send failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleNotifyEvent executes notify event flow and returns the first error.
func (b *bot) handleNotifyEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	var payload telegrambot.NotifyEvent
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid payload"})
		return
	}
	text := telegrambot.RenderNotifyMessage(payload)
	if err := b.sendToNotifyChats(r.Context(), text); err != nil {
		b.logger.Warn("send notify telegram message failed", "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "telegram send failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// sendToNotifyChats returns send to notify chats.
func (b *bot) sendToNotifyChats(ctx context.Context, text string) error {
	if len(b.notifyChats) == 0 {
		return errors.New("no notify chats configured")
	}
	var firstErr error
	for _, chatID := range b.notifyChats {
		if err := b.sendPlainMessage(ctx, chatID, text, nil); err != nil {
			b.logger.Warn("telegram sendMessage failed", "chat_id", chatID, "error", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}
