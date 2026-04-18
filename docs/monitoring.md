# Monitoring operations

Monitoring is optional and managed by `ovpn`.
Recommended host model is clean ovpn-only.

## Stack

- Prometheus
- Alertmanager
- Grafana
- node_exporter
- cAdvisor
- `ovpn-agent` metrics endpoint
- `ovpn-telegram-bot` (Alertmanager relay + operator audit/admin assistant)

## Dashboards

Default Grafana folder `ovpn` includes:

- `ovpn Host Overview`
- `ovpn Containers Overview`
- `ovpn Agent Overview`
- `ovpn User Statistics (Local Server)` (per-user traffic and quota state for the current server)

`ovpn Host Overview` and `ovpn Containers Overview` include capacity-oriented panels:

- memory available and inode usage
- root disk free forecast (`predict_linear`, 24h)
- CPU usage by core
- container memory percent of host by service
- top CPU services
- restart pressure (15m)

`ovpn User Statistics` uses per-user `email` labels from `ovpn-agent` metrics. Treat Grafana/Prometheus access as sensitive.
User identities are mirrored cluster-wide by CLI flows, but this dashboard remains local per server.
Expiry state is also exported per user and uses UTC end-of-day semantics.

Quota-related metrics are rolling-window semantics:

- `ovpn_agent_user_window_30d_usage_bytes`
- `ovpn_agent_user_window_30d_quota_bytes`
- `ovpn_agent_user_quota_percent`
- `ovpn_agent_user_quota_blocked`

Expiry-related metrics:

- `ovpn_agent_user_expiry_timestamp_seconds`
- `ovpn_agent_user_expired`
- `ovpn_agent_user_effective_enabled`
- `ovpn_agent_user_days_until_expiry`
- `ovpn_agent_users_expiring_2d`
- `ovpn_agent_users_expired`

Expiry semantics:

- operator input is date-only (`YYYY-MM-DD`)
- a date remains valid through the end of that UTC day
- the stored cutoff is the next day at `00:00:00 UTC`
- expired users are removed from effective runtime access even if their manual `enabled` flag is still `true`
- after an unexpected cold restart, enforcement is restored by the next agent startup/collector pass

## Alert profile (balanced default)

Default alerts cover:

- host resource pressure (CPU, memory, low memory available, disk, inodes, disk-fill forecast)
- container presence/restart bursts
- service pressure (`xray` high CPU, `prometheus` high memory, `grafana` high memory)
- agent health, collector runtime errors, cert expiry
- user expiry warning (`OVPNUserExpirySoon`) when an effectively enabled user is within the next 2 days of expiry
- bot health:
  - `OVPNTelegramBotDown`
  - `OVPNTelegramBotPollingStale`
  - `OVPNTelegramBotSendFailures`

Bot metrics exported by `ovpn-telegram-bot`:

- `ovpn_telegram_bot_poll_last_success_timestamp_seconds`
- `ovpn_telegram_bot_poll_failures_total`
- `ovpn_telegram_bot_poll_failures_consecutive`
- `ovpn_telegram_bot_send_last_success_timestamp_seconds`
- `ovpn_telegram_bot_send_failures_total`
- `ovpn_telegram_bot_send_failures_consecutive`
- `ovpn_telegram_bot_watchdog_unhealthy`

## Telegram architecture

- Alert flow: `Alertmanager -> webhook -> ovpn-telegram-bot -> Telegram`
- Change events:
  - `ovpn` CLI sends runtime operation events to bot `/notify`
  - `ovpn-agent` sends quota block/unblock events to bot `/notify`
- Audit model:
  - Telegram bot uses long polling (`getUpdates`)
  - Bot `/health` is now real liveness/readiness, not a static `200 OK`
  - Bot `/metrics` is scraped by Prometheus
  - Button taps are handled via `callback_query` + `answerCallbackQuery`
  - Default mode is read-only audit (`Status`, `Services`, `Doctor`, `Users`, `Traffic`, `Quota`)
  - Optional owner-only mutating actions (`/restart`, `/heal`) are enabled only when `monitoring/secrets/telegram_admin_token` is non-empty
  - Mutating actions use Docker socket access from bot container and require two-step confirmation in chat
  - Bot reads only internal endpoints (`ovpn-agent`, `prometheus`, `alertmanager`, `grafana`, `node-exporter`, `cadvisor`)
  - No public Telegram webhook endpoint is exposed
  - Bot surfaces local server traffic/quota view; global user identity mirroring is handled by CLI/state layer
  - `/users` shows expiry date and state (`no-exp`, `expiring`, `expired`)
  - `/doctor` includes `expiring_2d` and `expired` counts
  - Optional `User link` generation fails open: broken link config disables only that feature and does not stop alert delivery
  - Polling stale state triggers an in-process watchdog exit so Compose `restart: unless-stopped` can recover the bot

```text
Telegram <-> ovpn-telegram-bot (long polling)
                  |
                  +-> ovpn-agent (/health, /stats/*, /quota/*, /quota/policies)
                  +-> Prometheus (/-/healthy)
                  +-> Alertmanager (/-/healthy)
                  +-> Grafana (/api/health)
                  +-> node_exporter (/metrics)
                  +-> cAdvisor (/healthz)
                  +-> Docker API (/var/run/docker.sock, optional)
                  +-> local clients.pdf (Guide PDF sendDocument, generated from docs/clients.md)
```

## Start and check

```bash
./ovpn deploy <server>
./ovpn server monitor up <server>
./ovpn server monitor status <server>
```

Monitoring runtime defaults:

- Prometheus scrape/evaluation interval: `30s`
- Prometheus retention: `10d`
- cAdvisor housekeeping: `30s` (max `2m`)

If `monitoring/secrets/telegram_bot_token` is empty, `ovpn server monitor up` now starts the monitoring stack with `ovpn-telegram-bot` scaled to `0` (no restart loop).

Security note: enabling `telegram_admin_token` grants the bot container controlled Docker restart capability via `/var/run/docker.sock`. Keep owner account and token protected.

## Telegram setup

1. Create Telegram bot via `@BotFather`.
2. Put bot token on remote host:

```bash
ssh <ssh-user>@<server-ip> 'install -m 600 /dev/null /opt/ovpn/monitoring/secrets/telegram_bot_token'
ssh <ssh-user>@<server-ip> 'cat > /opt/ovpn/monitoring/secrets/telegram_bot_token'
```

3. Set Telegram env before deploy:

```bash
export OVPN_TELEGRAM_OWNER_USER_ID=<owner-user-id>  # recommended
export OVPN_TELEGRAM_NOTIFY_CHAT_IDS=<chat-id-1>

# Optional: enable owner-only /restart and /heal actions.
# export OVPN_TELEGRAM_ADMIN_TOKEN=<long-random-secret>

# Optional custom PDF path inside container
# export OVPN_TELEGRAM_CLIENTS_PDF_PATH=/opt/ovpn/monitoring/telegram-bot/assets/clients.pdf

# Optional when using non-default bot host port:
# export OVPN_TELEGRAM_NOTIFY_URL=http://127.0.0.1:19002/notify
```

Owner id fallback behavior:

- If `OVPN_TELEGRAM_OWNER_USER_ID` is empty during deploy, `ovpn` writes owner id from the first `OVPN_TELEGRAM_NOTIFY_CHAT_IDS` entry.
- This prevents `ovpn-telegram-bot` restart loops on re-deploys.
- Recommended: keep `OVPN_TELEGRAM_OWNER_USER_ID` explicit for predictable access control.
- If the link config file is missing or invalid after deploy, the bot still starts in alerting and audit mode and marks `User link` as disabled.

4. Re-deploy and (re)start monitoring:

```bash
./ovpn deploy <server>
./ovpn server monitor down <server>
./ovpn server monitor up <server>
```

One-shot setup command:

```bash
OVPN_TELEGRAM_BOT_TOKEN=<token> \
./ovpn server monitor telegram-setup <server> \
  --owner-user-id <owner-user-id>
```

`--owner-user-id` is optional in setup mode.
When omitted, setup falls back to the first notify chat id as owner.
If provided, `--owner-user-id` (and `OVPN_TELEGRAM_OWNER_USER_ID`) must be exactly one numeric Telegram user ID.

This command uploads the token file, deploys runtime with minimal Telegram env, starts monitoring, and sends a test notification.
If `--notify-chat-ids` is empty, setup defaults notify target to owner.

## Telegram menu and commands

Main reply keyboard:

- `Home`
- `Status`
- `Doctor`
- `Services`
- `Users`
- `Traffic`
- `Quota`
- `Help`

Inline submenus:

- Services:
  - `Overview`
  - `Doctor`
  - per-service drilldowns (`Agent`, `Xray`, `Prometheus`, `Alertmanager`, `Grafana`, `Node Exporter`, `cAdvisor`, `Bot Self`)
  - owner recovery buttons (`Heal Unhealthy`, `Restart ...`) when admin token is configured
- Users:
  - `Refresh`
  - `Top`
  - `User link`
  - `Back`
- Traffic:
  - `Totals`
  - `Top 10`
  - `Today`
  - `Back`
- Quota:
  - `Summary`
  - `Over 80%`
  - `Over 95%`
  - `Blocked`
  - `Back`

Slash command fallback:

- `/start`, `/menu`
- `/status`
- `/services`
- `/doctor`
- `/users`
  - shows local quota/traffic plus mirrored expiry state
- `/traffic`
- `/quota`
- `/restart <service>`
- `/heal`
- `/guide`
- `/help`
- `/cancel`

`User link` policy:

- Full `vless://...` output is owner-only (`OVPN_TELEGRAM_OWNER_USER_ID`).
- Bot accepts username or full email for lookup.
- Bot reads user status from `GET /users/status` and quota policy from `GET /quota/policies`.
- Link generation uses auto-generated `monitoring/telegram-bot/link-config.json` from server deploy data.
- Mutating actions are owner-only, require two-step confirmation, and are enabled only with `telegram_admin_token`.
- `/users`, `/traffic`, `/quota` report local server data; user identities are mirrored globally by `ovpn user` workflows.
- Expiry alerts are sent by Prometheus/Alertmanager/Telegram and repeat per normal alerting rules until resolved.

## Stop monitoring

```bash
./ovpn server monitor down <server>
```

## Access Grafana safely

```bash
ssh -L 3000:127.0.0.1:3000 <ssh-user>@<server-ip>
```

Then open `http://127.0.0.1:3000`.

## Logs

```bash
./ovpn server logs <server> --service prometheus --tail 200
./ovpn server logs <server> --service alertmanager --tail 200
./ovpn server logs <server> --service ovpn-telegram-bot --tail 200
./ovpn server logs <server> --service grafana --tail 200
./ovpn server logs <server> --service ovpn-agent --tail 200
```

## Image pin overrides

If you need custom image tags, set overrides before `ovpn deploy`:

- `OVPN_PROMETHEUS_IMAGE`
- `OVPN_ALERTMANAGER_IMAGE`
- `OVPN_GRAFANA_IMAGE`
- `OVPN_NODE_EXPORTER_IMAGE`
- `OVPN_CADVISOR_IMAGE`
- `OVPN_TELEGRAM_BOT_IMAGE`
