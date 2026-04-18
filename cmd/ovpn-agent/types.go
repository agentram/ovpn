package main

import (
	"context"

	"ovpn/internal/model"
)

type runtimeReq struct {
	InboundTag string `json:"inbound_tag"`
	Email      string `json:"email"`
	UUID       string `json:"uuid"`
}

type quotaSyncReq struct {
	Users []model.QuotaUserPolicy `json:"users"`
}

type usersSyncReq struct {
	Users []model.UserPolicy `json:"users"`
}

type quotaResetReq struct {
	Email string `json:"email"`
}

const telegramNotifyEndpoint = "http://ovpn-telegram-bot:8080/notify"

type quotaPolicyLister interface {
	ListQuotaPolicies(ctx context.Context) ([]model.QuotaUserPolicy, error)
}
