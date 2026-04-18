package main

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type botMetrics struct {
	pollLastSuccessUnix    prometheus.Gauge
	pollFailuresTotal      prometheus.Counter
	pollFailuresConsec     prometheus.Gauge
	sendLastSuccessUnix    prometheus.Gauge
	sendFailuresTotal      prometheus.Counter
	sendFailuresConsec     prometheus.Gauge
	watchdogUnhealthyState prometheus.Gauge
}

func newBotMetrics(reg prometheus.Registerer) *botMetrics {
	m := &botMetrics{
		pollLastSuccessUnix: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "ovpn_telegram_bot_poll_last_success_timestamp_seconds",
			Help: "Unix timestamp of the last successful Telegram getUpdates poll.",
		}),
		pollFailuresTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "ovpn_telegram_bot_poll_failures_total",
			Help: "Total number of Telegram getUpdates failures.",
		}),
		pollFailuresConsec: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "ovpn_telegram_bot_poll_failures_consecutive",
			Help: "Current count of consecutive Telegram getUpdates failures.",
		}),
		sendLastSuccessUnix: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "ovpn_telegram_bot_send_last_success_timestamp_seconds",
			Help: "Unix timestamp of the last successful outbound Telegram send.",
		}),
		sendFailuresTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "ovpn_telegram_bot_send_failures_total",
			Help: "Total number of outbound Telegram send failures.",
		}),
		sendFailuresConsec: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "ovpn_telegram_bot_send_failures_consecutive",
			Help: "Current count of consecutive outbound Telegram send failures.",
		}),
		watchdogUnhealthyState: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "ovpn_telegram_bot_watchdog_unhealthy",
			Help: "Bot watchdog unhealthy state derived from stale polling (1=unhealthy, 0=healthy/degraded).",
		}),
	}
	reg.MustRegister(
		m.pollLastSuccessUnix,
		m.pollFailuresTotal,
		m.pollFailuresConsec,
		m.sendLastSuccessUnix,
		m.sendFailuresTotal,
		m.sendFailuresConsec,
		m.watchdogUnhealthyState,
	)
	m.watchdogUnhealthyState.Set(0)
	return m
}

func (m *botMetrics) onPollSuccess(ts time.Time) {
	m.pollLastSuccessUnix.Set(float64(ts.UTC().Unix()))
	m.pollFailuresConsec.Set(0)
	m.watchdogUnhealthyState.Set(0)
}

func (m *botMetrics) onPollFailure(consecutive int) {
	m.pollFailuresTotal.Inc()
	m.pollFailuresConsec.Set(float64(consecutive))
}

func (m *botMetrics) onSendSuccess(ts time.Time) {
	m.sendLastSuccessUnix.Set(float64(ts.UTC().Unix()))
	m.sendFailuresConsec.Set(0)
}

func (m *botMetrics) onSendFailure(consecutive int) {
	m.sendFailuresTotal.Inc()
	m.sendFailuresConsec.Set(float64(consecutive))
}

func (m *botMetrics) setWatchdogUnhealthy(v bool) {
	if v {
		m.watchdogUnhealthyState.Set(1)
		return
	}
	m.watchdogUnhealthyState.Set(0)
}
