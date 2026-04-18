package main

import (
	"strings"
	"time"
)

// setPrompt applies prompt and returns an error on failure.
func (b *bot) setPrompt(chatID int64, kind string) {
	if strings.TrimSpace(kind) == "" {
		return
	}
	if b.prompts == nil {
		b.prompts = make(map[int64]promptState)
	}
	b.prompts[chatID] = promptState{Kind: kind, ExpiresAt: time.Now().UTC().Add(promptTTL)}
}

// getPrompt returns prompt for callers.
func (b *bot) getPrompt(chatID int64) (promptState, bool) {
	st, ok := b.prompts[chatID]
	if !ok {
		return promptState{}, false
	}
	if time.Now().UTC().After(st.ExpiresAt) {
		delete(b.prompts, chatID)
		return promptState{}, false
	}
	return st, true
}

// clearPrompt applies prompt and returns an error on failure.
func (b *bot) clearPrompt(chatID int64) {
	delete(b.prompts, chatID)
}

// setConfirm stores pending admin action confirmation for chat.
func (b *bot) setConfirm(chatID int64, kind string, services []string) {
	if strings.TrimSpace(kind) == "" {
		return
	}
	if b.confirms == nil {
		b.confirms = make(map[int64]confirmState)
	}
	cleanServices := make([]string, 0, len(services))
	for _, svc := range services {
		v := strings.TrimSpace(svc)
		if v == "" {
			continue
		}
		cleanServices = append(cleanServices, v)
	}
	b.confirms[chatID] = confirmState{
		Kind:      kind,
		Services:  cleanServices,
		ExpiresAt: time.Now().UTC().Add(confirmTTL),
	}
}

// getConfirm returns pending confirmation state for chat.
func (b *bot) getConfirm(chatID int64) (confirmState, bool) {
	st, ok := b.confirms[chatID]
	if !ok {
		return confirmState{}, false
	}
	if time.Now().UTC().After(st.ExpiresAt) {
		delete(b.confirms, chatID)
		return confirmState{}, false
	}
	return st, true
}

// clearConfirm clears pending confirmation state for chat.
func (b *bot) clearConfirm(chatID int64) {
	delete(b.confirms, chatID)
}

// isPromptInput reports whether prompt input.
func (b *bot) isPromptInput(text string) bool {
	clean := strings.TrimSpace(text)
	if clean == "" {
		return false
	}
	if strings.HasPrefix(clean, "/") {
		return false
	}
	return menuActionFromText(clean) == ""
}

// isCancelCommand reports whether cancel command.
func isCancelCommand(text string) bool {
	clean := strings.ToLower(strings.TrimSpace(text))
	return clean == "/cancel" || clean == "cancel"
}
