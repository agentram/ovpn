package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestTelegramClientAPIMethodsAndUploads(t *testing.T) {
	t.Parallel()

	var seen []string
	client := &telegramClient{
		token: "secret-token",
		http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			seen = append(seen, req.URL.Path)
			switch {
			case strings.HasSuffix(req.URL.Path, "/getUpdates"):
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":[{"update_id":7}]}`)), Header: make(http.Header)}, nil
			case strings.HasSuffix(req.URL.Path, "/sendMessage"),
				strings.HasSuffix(req.URL.Path, "/answerCallbackQuery"),
				strings.HasSuffix(req.URL.Path, "/sendDocument"),
				strings.HasSuffix(req.URL.Path, "/sendPhoto"):
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":{}}`)), Header: make(http.Header)}, nil
			default:
				return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(`{"ok":false}`)), Header: make(http.Header)}, nil
			}
		})},
	}

	updates, err := client.getUpdates(context.Background(), 3, 1)
	if err != nil {
		t.Fatalf("get updates: %v", err)
	}
	if len(updates) != 1 || updates[0].UpdateID != 7 {
		t.Fatalf("unexpected updates: %+v", updates)
	}
	if err := client.sendMessageWithMode(context.Background(), 11, "hello", map[string]any{"k": "v"}, "HTML"); err != nil {
		t.Fatalf("send message: %v", err)
	}
	if err := client.answerCallbackQuery(context.Background(), "cb-1", "done"); err != nil {
		t.Fatalf("answer callback: %v", err)
	}
	if err := client.answerCallbackQuery(context.Background(), " ", "ignored"); err != nil {
		t.Fatalf("blank callback should be ignored: %v", err)
	}
	if err := client.sendDocument(context.Background(), 11, "guide.txt", strings.NewReader("doc"), "guide"); err != nil {
		t.Fatalf("send document: %v", err)
	}
	if err := client.sendPhoto(context.Background(), 11, "qr.png", strings.NewReader("png"), "qr", map[string]any{"inline_keyboard": []any{}}); err != nil {
		t.Fatalf("send photo: %v", err)
	}
	if len(seen) < 5 {
		t.Fatalf("expected telegram API calls, got %+v", seen)
	}
}

func TestTelegramClientErrorsAndRedaction(t *testing.T) {
	t.Parallel()

	client := &telegramClient{
		token: "secret-token",
		http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch {
			case strings.HasSuffix(req.URL.Path, "/getUpdates"):
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":false,"description":"denied"}`)), Header: make(http.Header)}, nil
			case strings.HasSuffix(req.URL.Path, "/sendMessage"):
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":false,"description":"message denied"}`)), Header: make(http.Header)}, nil
			case strings.HasSuffix(req.URL.Path, "/sendDocument"):
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`not-json`)), Header: make(http.Header)}, nil
			case strings.HasSuffix(req.URL.Path, "/sendPhoto"):
				return nil, errors.New("post https://api.telegram.org/botsecret-token/sendPhoto failed")
			default:
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":false,"description":"callback denied"}`)), Header: make(http.Header)}, nil
			}
		})},
	}

	if _, err := client.getUpdates(context.Background(), 0, 0); err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("expected getUpdates API error, got %v", err)
	}
	if err := client.sendMessageWithMode(context.Background(), 11, "hello", nil, ""); err == nil || !strings.Contains(err.Error(), "message denied") {
		t.Fatalf("expected sendMessage API error, got %v", err)
	}
	if err := client.answerCallbackQuery(context.Background(), "cb", ""); err == nil || !strings.Contains(err.Error(), "callback denied") {
		t.Fatalf("expected callback API error, got %v", err)
	}
	if err := client.sendDocument(context.Background(), 11, "guide.txt", strings.NewReader("doc"), ""); err == nil {
		t.Fatalf("expected sendDocument decode error")
	}
	if err := client.sendPhoto(context.Background(), 11, "qr.png", strings.NewReader("png"), "", nil); err == nil || strings.Contains(err.Error(), "secret-token") || !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("expected redacted sendPhoto transport error, got %v", err)
	}
	if err := client.redactError(errors.New("secret-token leaked")); err == nil || err.Error() != "[REDACTED] leaked" {
		t.Fatalf("unexpected redacted error: %v", err)
	}
	if err := client.redactError(nil); err != nil {
		t.Fatalf("nil error should stay nil, got %v", err)
	}
}
