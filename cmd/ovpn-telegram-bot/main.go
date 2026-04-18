package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"ovpn/internal/logx"
	"ovpn/internal/telegrambot"
)

// main wires dependencies, starts workers, and blocks until shutdown.
func main() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(2)
	}

	level, err := logx.ParseLevel(cfg.logLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid log level: %v\n", err)
		os.Exit(2)
	}
	logger := logx.NewTextLogger(level).With("component", "ovpn-telegram-bot")
	slog.SetDefault(logger)

	token, err := readSecretFile(cfg.tokenFile)
	if err != nil {
		logger.Error("read telegram token failed", "token_file", cfg.tokenFile, "error", err)
		os.Exit(1)
	}
	adminToken, err := readOptionalSecretFile(cfg.adminTokenFile)
	if err != nil {
		logger.Warn("read admin token failed; mutating actions disabled", "admin_token_file", cfg.adminTokenFile, "error", err)
	}

	notifyChats, err := telegrambot.ParseIDSliceCSV(cfg.notifyChatIDs)
	if err != nil {
		logger.Error("invalid notify chat ids", "error", err)
		os.Exit(2)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	telegramClientHTTP := newTelegramHTTPClient(logger, cfg.telegramAPIFallbackIPs)
	if cfg.ownerUserID <= 0 {
		logger.Warn("owner user id is empty; owner-only interactive access is disabled")
	}
	reg := prometheus.NewRegistry()
	metrics := newBotMetrics(reg)
	var operator serviceOperator
	if strings.TrimSpace(adminToken) != "" {
		operator = newDockerServiceOperator("/var/run/docker.sock")
		logger.Info("telegram admin actions enabled")
	} else {
		logger.Info("telegram admin actions disabled; admin token is empty")
	}
	b := &bot{
		logger:     logger,
		cfg:        cfg,
		httpClient: client,
		tg:         &telegramClient{token: token, http: telegramClientHTTP},
		operator:   operator,
		notifyChats: append([]int64(nil),
			notifyChats...,
		),
		prompts:    map[int64]promptState{},
		confirms:   map[int64]confirmState{},
		adminToken: strings.TrimSpace(adminToken),
		health:     newBotHealth(cfg.pollInterval, metrics),
		metrics:    metrics,
		exitFn:     os.Exit,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", b.handleHealth)
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	mux.HandleFunc("/alertmanager", b.handleAlertmanagerWebhook)
	mux.HandleFunc("/notify", b.handleNotifyEvent)

	srv := &http.Server{Addr: cfg.listenAddr, Handler: withRequestLogging(logger, mux)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		logger.Info("telegram bot http server listening", "listen", cfg.listenAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("telegram bot http server failed", "error", err)
			os.Exit(1)
		}
	}()

	go b.pollLoop(ctx)
	go b.watchdogLoop(ctx)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	logger.Info("shutdown signal received")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Warn("graceful shutdown failed", "error", err)
	}
}
