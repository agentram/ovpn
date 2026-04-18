package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"ovpn/internal/logx"
	"ovpn/internal/stats"
	"ovpn/internal/store/remote"
	"ovpn/internal/telegrambot"
)

// main wires dependencies, starts workers, and blocks until shutdown.
func main() {
	var listen string
	var xrayAPI string
	var dbPath string
	var poll string
	var logLevelRaw string
	var certFile string
	var spikeDeltaBytes int64
	var debug bool

	flag.StringVar(&listen, "listen", ":9090", "HTTP listen address")
	flag.StringVar(&xrayAPI, "xray-api", "xray:10085", "Xray API address")
	flag.StringVar(&dbPath, "db-path", "/var/lib/ovpn-agent", "database directory")
	flag.StringVar(&poll, "poll-interval", "30s", "stats polling interval")
	flag.StringVar(&logLevelRaw, "log-level", strings.TrimSpace(os.Getenv("OVPN_AGENT_LOG_LEVEL")), "Log level: debug|info|warn|error (default: env OVPN_AGENT_LOG_LEVEL or info)")
	flag.StringVar(&certFile, "cert-file", strings.TrimSpace(os.Getenv("OVPN_AGENT_CERT_FILE")), "Optional path to fullchain certificate file for expiry monitoring")
	flag.Int64Var(&spikeDeltaBytes, "spike-delta-bytes", envInt64("OVPN_AGENT_SPIKE_DELTA_BYTES", stats.DefaultUserSpikeDeltaBytes), "Per-user delta threshold for spike events")
	flag.BoolVar(&debug, "debug", false, "Enable debug logging (shorthand for --log-level=debug)")
	flag.Parse()

	if debug {
		logLevelRaw = "debug"
	}
	level, err := logx.ParseLevel(logLevelRaw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid log level: %v\n", err)
		os.Exit(2)
	}
	logger := logx.NewTextLogger(level).With("component", "ovpn-agent")
	slog.SetDefault(logger)

	interval, err := time.ParseDuration(poll)
	if err != nil {
		logger.Error("invalid poll interval", "poll_interval", poll, "error", err)
		os.Exit(2)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store, err := remote.Open(ctx, dbPath)
	if err != nil {
		logger.Error("open remote store failed", "db_path", dbPath, "error", err)
		os.Exit(1)
	}
	defer store.Close()

	metrics := newAgentMetrics(prometheus.DefaultRegisterer)
	var runtimeMu sync.Mutex
	runtime := &runtimeGateway{
		apiAddr:  xrayAPI,
		mu:       &runtimeMu,
		logger:   logger,
		observer: metrics,
	}

	quotaEnforcer := &stats.QuotaEnforcer{
		Store:                     store,
		Runtime:                   runtime,
		DefaultWindow30DQuotaByte: stats.DefaultWindow30DQuotaBytes,
		Window:                    stats.DefaultQuotaWindow,
		Logger:                    logger,
		OnEvent:                   metrics.observeQuotaEvent,
		OnBlockedUsers:            metrics.setQuotaBlockedUsers,
		OnUsageBands:              metrics.setQuotaUsageBands,
		OnNotify: func(event, message string) {
			payload := telegrambot.NotifyEvent{
				Event:    event,
				Status:   "success",
				Severity: "warning",
				Source:   "ovpn-agent",
				Message:  message,
			}
			if err := postNotifyEvent(context.Background(), payload); err != nil {
				logger.Debug("telegram quota notify skipped", "event", event, "error", err)
			}
		},
	}
	expiryEnforcer := &stats.ExpiryEnforcer{
		Store:   store,
		Runtime: runtime,
		Logger:  logger,
	}

	collector := &stats.Collector{
		Store:           store,
		APIAddr:         xrayAPI,
		Interval:        interval,
		InboundTag:      "vless-reality",
		SpikeDeltaBytes: spikeDeltaBytes,
		Logger:          logger,
		Observer:        metrics,
		Quota:           quotaEnforcer,
		Expiry:          expiryEnforcer,
	}
	go func() {
		if err := collector.Run(ctx); err != nil && err != context.Canceled {
			logger.Warn("collector stopped with error", "error", err)
		}
	}()

	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		metrics.observeCertExpiry(certFile, logger)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				metrics.observeCertExpiry(certFile, logger)
			}
		}
	}()

	refreshMetrics := newMetricsRefreshFunc(store, logger, metrics)
	refreshMetrics(ctx)
	if err := expiryEnforcer.Enforce(ctx, time.Now().UTC()); err != nil {
		logger.Warn("initial expiry enforcement failed", "error", err)
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				refreshMetrics(ctx)
			}
		}
	}()

	mux := http.NewServeMux()
	registerHTTPRoutes(ctx, mux, routeDeps{
		store:       store,
		collector:   collector,
		quota:       quotaEnforcer,
		expiry:      expiryEnforcer,
		runtime:     runtime,
		metrics:     metrics,
		logger:      logger,
		xrayAPI:     xrayAPI,
		dbPath:      dbPath,
		refreshOnce: refreshMetrics,
	})

	srv := &http.Server{Addr: listen, Handler: withRequestLogging(logger, mux)}
	go func() {
		logger.Info("ovpn-agent listening", "listen", listen, "xray_api", xrayAPI, "poll_interval", interval.String(), "db_path", dbPath, "cert_file", certFile, "log_level", level.String())
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server failed", "error", err)
			os.Exit(1)
		}
	}()

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
