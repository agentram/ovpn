package main

import (
	"log/slog"
	"net/http"
	"sync"
	"time"
)

const (
	menuHome     = "Home"
	menuStatus   = "Status"
	menuDoctor   = "Doctor"
	menuServices = "Services"
	menuUsers    = "Users"
	menuTraffic  = "Traffic"
	menuQuota    = "Quota"
	menuHelp     = "Help"

	promptUserLink = "user_link"
	promptTTL      = 5 * time.Minute
	confirmTTL     = 90 * time.Second
)

type config struct {
	listenAddr      string
	agentURL        string
	prometheusURL   string
	alertmanagerURL string
	grafanaURL      string
	nodeExporterURL string
	cadvisorURL     string
	selfURL         string
	tokenFile       string
	adminTokenFile  string
	notifyChatIDs   string
	pollInterval    time.Duration
	logLevel        string

	ownerUserID    int64
	clientsPDFPath string
	linkConfigFile string
	linkAddress    string
	linkServerName string
	linkPublicKey  string
	linkShortID    string
	linkConfigErr  string

	telegramAPIFallbackIPs []string
}

type bot struct {
	logger *slog.Logger
	cfg    config

	httpClient *http.Client
	tg         *telegramClient
	operator   serviceOperator

	notifyChats []int64
	prompts     map[int64]promptState
	confirms    map[int64]confirmState
	adminToken  string
	health      *botHealth
	metrics     *botMetrics
	exitFn      func(int)
}

type promptState struct {
	Kind      string
	ExpiresAt time.Time
}

type confirmState struct {
	Kind      string
	Services  []string
	ExpiresAt time.Time
}

type telegramClient struct {
	token string
	http  *http.Client
}

type telegramAPIResponse[T any] struct {
	OK          bool   `json:"ok"`
	Result      T      `json:"result"`
	Description string `json:"description"`
}

type telegramUpdate struct {
	UpdateID      int64                  `json:"update_id"`
	Message       *telegramMessage       `json:"message"`
	CallbackQuery *telegramCallbackQuery `json:"callback_query"`
}

type telegramCallbackQuery struct {
	ID      string           `json:"id"`
	From    *telegramUser    `json:"from"`
	Message *telegramMessage `json:"message"`
	Data    string           `json:"data"`
}

type telegramMessage struct {
	MessageID int64         `json:"message_id"`
	From      *telegramUser `json:"from"`
	Chat      telegramChat  `json:"chat"`
	Text      string        `json:"text"`
}

type telegramUser struct {
	ID int64 `json:"id"`
}

type telegramChat struct {
	ID int64 `json:"id"`
}

type agentHealth struct {
	OK               bool   `json:"ok"`
	Service          string `json:"service"`
	XrayAPI          string `json:"xray_api"`
	XrayAPIReachable bool   `json:"xray_api_reachable"`
	XrayAPIError     string `json:"xray_api_error"`
	DBPath           string `json:"db_path"`
	LastCollectAt    string `json:"last_collect_at"`
	LastResetAt      string `json:"last_reset_at"`
	Time             string `json:"time"`
}

type monitorStatus struct {
	Name    string
	Healthy bool
	Detail  string
}

type selfHealthResponse struct {
	OK          bool              `json:"ok"`
	Status      string            `json:"status"`
	LinkFeature string            `json:"link_feature"`
	Health      botHealthSnapshot `json:"health"`
}

type botHealth struct {
	mu                      sync.RWMutex
	startedAt               time.Time
	pollInterval            time.Duration
	lastPollSuccess         time.Time
	lastPollFailure         string
	consecutivePollFailures int
	lastSendSuccess         time.Time
	lastSendFailure         string
	consecutiveSendFailures int
	watchdogUnhealthy       bool
	metrics                 *botMetrics
}

type botHealthSnapshot struct {
	Status                  string    `json:"status"`
	OK                      bool      `json:"ok"`
	StartedAt               time.Time `json:"started_at"`
	LastPollSuccess         time.Time `json:"last_poll_success"`
	LastSendSuccess         time.Time `json:"last_send_success"`
	LastPollFailure         string    `json:"last_poll_failure,omitempty"`
	LastSendFailure         string    `json:"last_send_failure,omitempty"`
	ConsecutivePollFailures int       `json:"consecutive_poll_failures"`
	ConsecutiveSendFailures int       `json:"consecutive_send_failures"`
	PollStaleAfter          string    `json:"poll_stale_after"`
	WatchdogUnhealthy       bool      `json:"watchdog_unhealthy"`
}
