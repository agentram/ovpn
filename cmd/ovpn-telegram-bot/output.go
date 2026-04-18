package main

import (
	"context"
	"errors"
	"html"
	"io"
	"strings"
	"time"
)

const telegramMessageLimit = 3500

func (b *bot) linkFeatureStatus() string {
	if strings.TrimSpace(b.cfg.linkConfigErr) != "" {
		return "disabled"
	}
	if strings.TrimSpace(b.cfg.linkAddress) == "" || strings.TrimSpace(b.cfg.linkServerName) == "" || strings.TrimSpace(b.cfg.linkPublicKey) == "" || strings.TrimSpace(b.cfg.linkShortID) == "" {
		return "disabled"
	}
	return "enabled"
}

func (b *bot) sendPlainMessage(ctx context.Context, chatID int64, text string, replyMarkup any) error {
	return b.sendMessageWithMode(ctx, chatID, text, replyMarkup, "")
}

func (b *bot) sendHTMLMessage(ctx context.Context, chatID int64, text string, replyMarkup any) error {
	return b.sendMessageWithMode(ctx, chatID, text, replyMarkup, "HTML")
}

func (b *bot) sendMarkdownMessage(ctx context.Context, chatID int64, text string, replyMarkup any) error {
	return b.sendMessageWithMode(ctx, chatID, text, replyMarkup, "Markdown")
}

func (b *bot) sendMessageWithMode(ctx context.Context, chatID int64, text string, replyMarkup any, parseMode string) error {
	err := b.tg.sendMessageWithMode(ctx, chatID, text, replyMarkup, parseMode)
	if err != nil {
		b.ensureHealth().onSendFailure(err)
		return err
	}
	b.ensureHealth().onSendSuccess(time.Now().UTC())
	return nil
}

func (b *bot) sendDocument(ctx context.Context, chatID int64, filename string, src io.Reader, caption string) error {
	err := b.tg.sendDocument(ctx, chatID, filename, src, caption)
	if err != nil {
		b.ensureHealth().onSendFailure(err)
		return err
	}
	b.ensureHealth().onSendSuccess(time.Now().UTC())
	return nil
}

func (b *bot) sendHTMLChunks(ctx context.Context, chatID int64, messages []string, replyMarkup any) error {
	if len(messages) == 0 {
		return errors.New("empty telegram HTML payload")
	}
	for i, msg := range messages {
		markup := any(nil)
		if i == len(messages)-1 {
			markup = replyMarkup
		}
		if err := b.sendHTMLMessage(ctx, chatID, msg, markup); err != nil {
			return err
		}
	}
	return nil
}

func escapeTelegramHTML(v string) string {
	return html.EscapeString(strings.TrimSpace(v))
}

func truncateText(v string, width int) string {
	v = strings.TrimSpace(v)
	if width <= 0 {
		return ""
	}
	runes := []rune(v)
	if len(runes) <= width {
		return v
	}
	if width == 1 {
		return string(runes[:1])
	}
	return string(runes[:width-1]) + "…"
}

func padText(v string, width int) string {
	return padRight(truncateText(v, width), width)
}

func padRight(v string, width int) string {
	runes := []rune(v)
	if len(runes) >= width {
		return v
	}
	return v + strings.Repeat(" ", width-len(runes))
}

func buildPreformattedMessages(title string, totals string, header string, rows []string, limit int) []string {
	if limit <= 0 {
		limit = telegramMessageLimit
	}
	prefix := "<b>" + escapeTelegramHTML(title) + "</b>\n<pre>"
	suffix := "</pre>"
	lines := make([]string, 0, len(rows)+2)
	if strings.TrimSpace(totals) != "" {
		lines = append(lines, totals, "")
	}
	if strings.TrimSpace(header) != "" {
		lines = append(lines, header)
	}
	lines = append(lines, rows...)

	chunks := make([]string, 0, 1)
	current := ""
	for _, line := range lines {
		candidate := line
		if current != "" {
			candidate = current + "\n" + line
		}
		if len(prefix)+len(candidate)+len(suffix) <= limit {
			current = candidate
			continue
		}
		if current != "" {
			chunks = append(chunks, prefix+current+suffix)
		}
		current = line
	}
	if current != "" || len(chunks) == 0 {
		chunks = append(chunks, prefix+current+suffix)
	}
	return chunks
}
