# Changelog

All notable changes to this project will be documented in this file.

The format is based on Keep a Changelog and this repository uses plain semantic versions without a `v` prefix.

## 1.2.0

### Added
- Additive HA proxy topology with a new `proxy` server role, backend attachment commands, proxy-aware Xray rendering, and local HAProxy failover.
- Country-specific proxy presets for HA, with `ru` as the first built-in preset and future presets extensible on the same `proxy` role.
- Proxy-specific observability surfaces including Prometheus scrape config, HAProxy alerts, Grafana HA dashboard, and Telegram bot service awareness.
- Proxy rollout and operations documentation for Ansible bootstrap, deployment order, monitoring, troubleshooting, and failure model.

### Changed
- Plain `vpn` deployments no longer carry dormant proxy relay runtime identities unless the backend is actually attached to a proxy.
- Proxy rollout docs now require backend deploy after attachment so HA service identity reaches the backend runtime before proxy traffic is sent.
- Monitoring docs now describe the proxy dashboard as proxy-only instead of a universal dashboard.

### Fixed
- Proxy relay now targets the HAProxy service correctly and HAProxy binds on the container network so proxy-to-backend traffic can flow.
- Deploy and doctor remote validation paths now fail explicitly instead of hanging indefinitely on slow remote compose checks.
- Normal deploys preserve an already running monitoring stack instead of pruning `ovpn-telegram-bot`.

## 1.1.0

### Added
- User expiration dates with UTC end-of-day semantics, Telegram visibility, and Prometheus/Grafana alerting.
- Global-by-default user mirroring across enabled servers with reconcile support and REALITY parity checks.
- Owner-confirmed Telegram recovery actions for restart and heal flows.
- Pinned repository versioning with `VERSION`, `CHANGELOG.md`, and automated plain-semver releases.
- Public community scaffolding: issue forms, discussion forms, PR template, contributor guide, and template validation.
- Generated-document tooling for rebuilding the optional VPN client PDF locally instead of tracking the binary artifact.

### Changed
- Telegram bot UX is now operations-first with compact status, services, doctor, and user audit flows.
- Monitoring stack now exposes richer service checks and expiry-aware diagnostics.
- User identities default to globally mirrored email addressing to avoid server-specific drift.
- Renamed `README.codex.md` to `DEVELOPMENT.md` and rewrote contributor-facing docs for a public audience.
- Removed deploy, backup, and restore GitHub Actions workflows from the public repository boundary.
- Refreshed pinned runtime and monitoring versions to current stable releases in safe major lines.
- Release automation now creates a new public release automatically when `VERSION` and `CHANGELOG.md` are updated together on `main`.

### Fixed
- Bot owner detection now falls back safely when the explicit owner user id is missing.
- Expiry updates no longer trigger redundant runtime add operations for already-active users.
- Existing drift and cleanup paths were hardened for multi-server user mirroring.
- GitHub issue and discussion templates were simplified and validated against GitHub form requirements.
- Repository hygiene checks now block tracked generated PDFs, local workstation paths, private inventories, and common secret patterns.
- Root-disk fill prediction alert now requires both low free space and a negative trend to reduce false positives.

## 1.0.0

### Added
- Initial stable release of the `ovpn` CLI, `ovpn-agent`, monitoring stack, Grafana dashboards, and Telegram bot.
