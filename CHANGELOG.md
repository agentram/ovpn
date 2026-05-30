# Changelog

All notable changes to this project will be documented in this file.

The format is based on Keep a Changelog and this repository uses plain semantic versions without a `v` prefix.

## 1.5.0

### Security
- `ovpn-agent` mutating endpoints (`/quota/*`, `/users/sync`, `/runtime/user/*`, `/collect`) now require a bearer token when `OVPN_AGENT_TOKEN` is set. The CLI auto-generates and persists a token under `~/.ovpn/secrets/agent-token` and renders it into the remote `.env`, so deploys enable it automatically. Read-only endpoints (`/metrics`, `/health`, stats/status) stay open for Prometheus and health checks.
- Rendered `xray/config.json` is now delivered as `0640 root:<xray-gid>` (was world-readable `0644`); it embeds the REALITY private key and client UUIDs, so it is now readable only by root and the xray runtime group.
- Added a `govulncheck` job to the security workflow.

### Changed
- Deploy bundles extract with `tar --no-same-owner`, so remote files are owned by the deploying account instead of the operator's local UID baked into the archive.
- Repo-root `docker-compose.yml`/`docker-compose.monitoring.yml` are kept byte-identical to the embedded deploy templates (guarded by a test); the duplicate, drifted `deploy/compose/` tree was removed.
- Both HTTP servers (`ovpn-agent`, `ovpn-telegram-bot`) now set read/write/idle timeouts.

### Fixed
- Traffic deltas and their source counter are now persisted in a single transaction, preventing double-counting if a write fails between them.

## 1.4.5

### Added
- Split VPN client documentation into separate English and Russian guides.
- Telegram `/guide` now sends both English and Russian client PDF guides when both assets are available.

### Changed
- `make docs-pdf` now builds both `clients.pdf` and `clients-ru.pdf`.

## 1.4.4

### Changed
- Added focused unit coverage across CLI, agent, Telegram bot, stores, stats, SSH, backup, runtime assets, and version helpers.

### Fixed
- Release self-contained dry-run deploy smoke no longer calls remote agent HTTP endpoints while `--dry-run` is active.
- Dry-run deploy rendering no longer reads remote quota state from an already deployed server, keeping dry-run preview local-only.

## 1.4.3

### Added
- Added `ovpn user quota-set --monthly-gb <gb>` for human-sized rolling `30d` quota changes.
- Added stronger unit coverage for quota parsing, user command argument validation, terminal QR rendering, remote agent HTTP parsing, and runtime user removal.

### Changed
- Refreshed README and GitHub Pages content for engineer-focused operations: Docker runtime, local desired state, SSH/SCP automation, HA proxy topology, security defaults, Ansible hardening, CI badges, fast navigation, and architecture diagrams.
- Terminal QR output now uses a smaller QR encoding and trims the quiet zone without corrupting multi-byte terminal characters.
- Remote agent HTTP calls now parse HTTP status explicitly so fallback-capable runtime mutations do not surface agent `500` responses as SSH command failures.

### Fixed
- Runtime user removal is idempotent when the user is already absent from Xray, avoiding unnecessary scary errors before deploy fallback.
- User subcommands now reject unexpected positional arguments instead of silently ignoring mistyped input.

## 1.4.2

### Fixed
- Telegram bot stale polling now degrades health metrics without failing the Docker healthcheck, avoiding restart loops during temporary Telegram API outages.

## 1.4.1

### Added
- Telegram bot user-link generation now sends a QR image after the owner-only `vless://` link.
- Added the `cn` proxy preset for China split routing, with `china` accepted as an alias.

### Changed
- Release archives now contain only the `ovpn` CLI because `ovpn-agent` and `ovpn-telegram-bot` are embedded in release builds for deploy and Telegram setup.
- Terminal QR output for `ovpn user link` is smaller while keeping the link as the first stdout line.

## 1.4.0

### Added
- Embedded Linux `ovpn-agent` and `ovpn-telegram-bot` runtime binaries in release `ovpn` builds, so normal deploys no longer require a local Go toolchain or source checkout.
- Added default terminal QR output to `ovpn user link`, `--qr=false` for link-only output, and `--qr-file <path.png>` for saved PNG QR codes.

### Changed
- Release builds now package `ovpn-telegram-bot` alongside `ovpn` and `ovpn-agent`, and run a self-contained deploy smoke test without `go` in `PATH`.

## 1.3.3

### Changed
- Simplified public documentation for first-time users, local proof-of-concept setups, and small VPS/VDS deployments.
- Updated the pinned CodeQL SARIF upload action from `4.35.3` to `4.35.5`.

## 1.3.2

### Changed
- Increased the default rolling `30d` quota for users from `200 GB` to `300 GB`.
- Updated quota documentation and CLI help text to match the new default.

## 1.3.1

### Changed
- Refreshed Go module dependencies, including the CLI table renderer, gRPC, modernc SQLite, Prometheus support libraries, and `golang.org/x` modules.
- Documented source-root resolution for installed `ovpn` binaries that run `init` or `deploy` outside the repository checkout.

### Fixed
- Installed `ovpn` binaries now resolve the project source root before building local runtime binaries, so `server init` no longer fails with a missing `go.mod` when launched from another directory.
- Deploy workflows now validate local runtime binary builds before bootstrapping a remote host.

## 1.3.0

### Changed
- Added an Ansible host-maintenance playbook for already-deployed hosts so host baseline changes can be applied without rewriting runtime scaffolding.
- Docker daemon management now merges live-restore and default json-file log rotation into existing daemon config for more predictable maintenance behavior.
- SSH hardening now disables agent forwarding by default while preserving explicit TCP forwarding overrides for monitoring tunnels.
- Host maintenance now supports optional declared apt source/package cleanup and obsolete UFW allow cleanup.
- Login MOTD now shows OVPN service role, domain, deploy root, public VPN port, monitoring tunnel policy, and no-auto-reboot maintenance policy.
- Monitoring compose now gives node_exporter/cAdvisor host metadata mounts and aligns Alertmanager retention with weekly expiry alert repeats.

### Fixed
- Existing OVPN runtime secret files and backup archives are locked down when present without failing on fresh hosts where those files do not exist yet.
- Deploy now preserves the same runtime file permissions expected by host maintenance for `.env` and Xray config, avoiding permission drift after redeploys.
- Generated Grafana provisioning now creates empty alerting/plugins directories to avoid missing-directory startup noise.

## 1.2.1

### Changed
- Grafana dashboards now present sparse operational events as zero instead of empty panels, hide noisy Prometheus internals in user tables, and treat missing certificate monitoring as not configured rather than expired.
- Container dashboard memory-percent panels now preserve per-service labels when dividing by host memory.
- Proxy HA dashboard now distinguishes regular VPN hosts from proxy hosts instead of showing HAProxy as a broken scrape target on non-proxy deployments.
- Ansible unattended-upgrades policy is now explicitly managed as security-only by default, with normal Ubuntu updates, Docker updates, and reboots left for manual maintenance.

### Fixed
- Host-specific unattended-upgrades package blacklists are now inventory-driven instead of hard-coded into the public defaults.
- Container presence cards now display boolean `1`/`0` values instead of raw `container_last_seen` Unix timestamps.

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
