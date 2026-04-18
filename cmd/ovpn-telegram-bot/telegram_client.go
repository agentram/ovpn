package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
)

// getUpdates returns updates for callers.
func (c *telegramClient) getUpdates(ctx context.Context, offset int64, timeoutSeconds int) ([]telegramUpdate, error) {
	payload := map[string]any{
		"offset":          offset,
		"timeout":         timeoutSeconds,
		"allowed_updates": []string{"message", "callback_query"},
	}
	var out telegramAPIResponse[[]telegramUpdate]
	if err := c.callTelegram(ctx, "getUpdates", payload, &out); err != nil {
		return nil, err
	}
	if !out.OK {
		return nil, errors.New(defaultText(out.Description, "telegram getUpdates failed"))
	}
	return out.Result, nil
}

func (c *telegramClient) sendMessageWithMode(ctx context.Context, chatID int64, text string, replyMarkup any, parseMode string) error {
	payload := map[string]any{
		"chat_id":                  chatID,
		"text":                     text,
		"disable_web_page_preview": true,
	}
	if strings.TrimSpace(parseMode) != "" {
		payload["parse_mode"] = parseMode
	}
	if replyMarkup != nil {
		payload["reply_markup"] = replyMarkup
	}
	var out telegramAPIResponse[map[string]any]
	if err := c.callTelegram(ctx, "sendMessage", payload, &out); err != nil {
		return err
	}
	if !out.OK {
		return errors.New(defaultText(out.Description, "telegram sendMessage failed"))
	}
	return nil
}

// answerCallbackQuery returns answer callback query.
func (c *telegramClient) answerCallbackQuery(ctx context.Context, callbackID string, text string) error {
	callbackID = strings.TrimSpace(callbackID)
	if callbackID == "" {
		return nil
	}
	payload := map[string]any{"callback_query_id": callbackID}
	if strings.TrimSpace(text) != "" {
		payload["text"] = text
		payload["show_alert"] = false
	}
	var out telegramAPIResponse[any]
	if err := c.callTelegram(ctx, "answerCallbackQuery", payload, &out); err != nil {
		return err
	}
	if !out.OK {
		return errors.New(defaultText(out.Description, "telegram answerCallbackQuery failed"))
	}
	return nil
}

// sendDocument handles send document HTTP behavior for this service.
func (c *telegramClient) sendDocument(ctx context.Context, chatID int64, filename string, src io.Reader, caption string) error {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("chat_id", strconv.FormatInt(chatID, 10)); err != nil {
		return err
	}
	if strings.TrimSpace(caption) != "" {
		if err := mw.WriteField("caption", caption); err != nil {
			return err
		}
	}
	part, err := mw.CreateFormFile("document", filename)
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, src); err != nil {
		return err
	}
	if err := mw.Close(); err != nil {
		return err
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendDocument", c.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &body)
	if err != nil {
		return c.redactError(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := c.http.Do(req)
	if err != nil {
		return c.redactError(err)
	}
	defer resp.Body.Close()
	var out telegramAPIResponse[map[string]any]
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return c.redactError(err)
	}
	if !out.OK {
		return errors.New(defaultText(out.Description, "telegram sendDocument failed"))
	}
	return nil
}

// callTelegram handles call telegram HTTP behavior for this service.
func (c *telegramClient) callTelegram(ctx context.Context, method string, payload any, out any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("https://api.telegram.org/bot%s/%s", c.token, method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return c.redactError(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return c.redactError(err)
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}

// redactError returns redact error.
func (c *telegramClient) redactError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if c != nil && strings.TrimSpace(c.token) != "" {
		msg = strings.ReplaceAll(msg, c.token, "[REDACTED]")
	}
	return errors.New(msg)
}
