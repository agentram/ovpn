# ovpn

`ovpn` is a Go CLI + agent system for operating self-hosted Xray (`VLESS + REALITY`) over SSH.

## Production model

Checked-in Ansible inventory is example-only. Keep real hostnames, IPs, and host variables private under `ansible/inventories/production` on your machine or in a separate private repo.

- Ansible prepares host baseline and security.
- `ovpn` manages VPN runtime, users, monitoring, quotas, and backups.
- Xray REALITY runs directly on `443/tcp`.
- Recommended flow keeps SSH on `22`.
- Monitoring is optional. The public repository ships only CI, security, and release automation; operational workflows stay private or local-only.
- Runtime security profile defaults to `minimal`.
- Recommended host model is clean ovpn-only.

## Key features

- SSH-based control plane (no public admin API)
- Local desired state in `~/.ovpn`
- Remote runtime via Docker Compose in `/opt/ovpn`
- User lifecycle and VLESS link generation
- Global-by-default user provisioning across all enabled servers
- Traffic stats and rolling `30d` quota enforcement
- Optional monitoring stack (Prometheus, Alertmanager, Grafana, Telegram bot relay)
- Built-in Grafana user analytics dashboard (`ovpn User Statistics`)
- Additive HA proxy topology with split routing and backend failover
- Country-specific proxy presets for HA, with Russia (`ru`) as the first built-in preset
- Built-in backup and restore commands
- Safe runtime cleanup/decommission command

## Versioning

- Current pinned version: `1.2.0`
- Check locally: `./ovpn version`
- Release source of truth:
  - `VERSION`
  - top entry in `CHANGELOG.md`
- Runtime image defaults source of truth:
  - [`internal/defaults/images.go`](internal/defaults/images.go)
- Semver policy:
  - major: breaking CLI, API, or operator workflow changes
  - minor: new commands, monitoring surfaces, or operator features
  - patch: fixes and smaller UX improvements without new public capability

## Community

- Issues: confirmed bugs and concrete feature requests
- Discussions: questions, usage help, design ideas, and announcements
- Security: use private vulnerability reporting, not public issues

## Requirements

- Host OS: Debian `12+` (including `13+`) or Ubuntu `22.04+` (including `24.04+`)
- SSH key access to target host
- Docker/Compose installed on host (via Ansible bootstrap)
- Go `1.26.2+` to build from source

## Security and quota defaults

- Security profile: `OVPN_SECURITY_PROFILE=minimal` (default)
- Minimal profile adds:
  - Xray routing block for `protocol=bittorrent`
  - Xray routing block for `geosite:category-public-tracker`
  - threat DNS servers from `OVPN_THREAT_DNS_SERVERS` (default `9.9.9.9,149.112.112.112`)
- Fast rollback if geosite resources are missing in image:
  - `export OVPN_SECURITY_PROFILE=off`
  - `./ovpn deploy <server>`
- Default user quota is rolling `30d`, `200 GB` when per-user limit is not explicitly set.
- Optional host-level Tor exit-node blocking is available in Ansible (`ovpn_block_tor_exit_nodes`), default `off`.
- Host hardening defaults now include:
  - journald cap (`200M` system, `100M` runtime),
  - swapfile `1 GB` with `vm.swappiness=10`,
  - purge of legacy nginx packages.

## Capacity defaults

- Backup retention is automatic:
  - remote server archives: keep latest `7` (`/opt/ovpn-backups/<server>-*.tgz`)
  - local archives: keep latest `7` (`~/.ovpn/backups/ovpn-local-*.tgz`)
  - pre-deploy snapshots `ovpn-*`: keep latest `7`
- Monitoring resource profile defaults:
  - Prometheus scrape/evaluation interval: `30s`
  - Prometheus retention: `10d`
  - cAdvisor housekeeping: `30s` (max `2m`)

## Quick start

```bash
go build -o ovpn ./cmd/ovpn
./ovpn version

./ovpn server add \
  --name <server> \
  --host <server-ip> \
  --domain <domain> \
  --ssh-user root \
  --ssh-port 22 \
  --xray-version 26.3.27

./ovpn server init <server>
./ovpn deploy <server>

./ovpn doctor <server>
./ovpn server status <server>
```

## Production flow (recommended)

Architecture path:

`Ansible -> ovpn -> optional monitoring -> optional CI workflows`

HA extension path:

`existing vpn servers -> Russia proxy role -> attached backend pool -> optional monitoring`

### 1. Pre-flight checks

```bash
ssh root@<server-ip> 'hostnamectl'
ssh root@<server-ip> 'sudo ss -lntup'
ssh root@<server-ip> 'sudo docker ps --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"'
```

### 2. Host bootstrap with Ansible

```bash
cd ansible
ANSIBLE_CONFIG=ansible.cfg ansible-playbook -i inventories/example/hosts.yml playbooks/bootstrap.yml --syntax-check
ANSIBLE_CONFIG=ansible.cfg ansible-playbook -i inventories/example/hosts.yml playbooks/bootstrap.yml --limit <host> --check --diff
ANSIBLE_CONFIG=ansible.cfg ansible-playbook -i inventories/example/hosts.yml playbooks/bootstrap.yml --limit <host>
```

Start from the checked-in example inventory, then copy it privately to `ansible/inventories/production` before using real infrastructure values.

Temporary non-ovpn exceptions (for example Zabbix `10050/tcp`) should be declared per-host via `ovpn_firewall_extra_tcp_ports`, not as global defaults.

### 3. Register server in local state

```bash
./ovpn server add \
  --name <server> \
  --host <server-ip> \
  --domain <domain> \
  --ssh-user root \
  --ssh-port 22 \
  --xray-version 26.3.27
```

### 4. Bootstrap and deploy runtime

```bash
./ovpn server init <server>
./ovpn deploy <server>
```

`init` vs `deploy`:

- `ovpn server init <server>`: first-time setup on a host (`bootstrap + deploy`).
- `ovpn deploy <server>`: normal re-deploy (`deploy` only, no bootstrap step).

Optional overrides for runtime security profile:

```bash
export OVPN_SECURITY_PROFILE=minimal
export OVPN_THREAT_DNS_SERVERS=9.9.9.9,149.112.112.112
./ovpn deploy <server>
```

### 4a. Additive HA proxy rollout

Use this only when adding a preset-driven `proxy` in front of existing foreign `vpn` servers.
Today the built-in preset is `ru`, which keeps Russian destinations local to the proxy and relays everything else through foreign backends.
If `--proxy-preset` is omitted for a proxy server, it defaults to `ru` for backward compatibility.
This does not modify the current direct path for existing users.

```bash
./ovpn server add \
  --name proxy-ru \
  --role proxy \
  --proxy-preset ru \
  --host <proxy-ip> \
  --domain <proxy-domain> \
  --ssh-user root \
  --ssh-port 22

./ovpn server init proxy-ru
./ovpn server backend attach --proxy proxy-ru --backend <vpn-backend-1>
./ovpn deploy <vpn-backend-1>
./ovpn config validate --server proxy-ru
./ovpn deploy proxy-ru
./ovpn doctor proxy-ru
```

After the first backend attachment, deploy that backend before deploying the proxy so the backend runtime picks up the proxy relay service identity used by HA.

On a proxy node, `ovpn deploy` renders and starts:

- `xray` for the public client entrypoint and split routing
- `haproxy` for local TCP failover across attached foreign backends
- `ovpn-agent` for runtime control and metrics
- optional monitoring services if enabled

See [`docs/ha.md`](docs/ha.md) for the full HA design, rollout, and failure model.

### 5. Validate runtime

```bash
./ovpn doctor <server>
./ovpn server status <server>
./ovpn server logs <server> --service xray --tail 200
./ovpn server logs <server> --service ovpn-agent --tail 200
```

### 6. Add users and deliver links

```bash
# Global-by-default: mutating user commands apply to all enabled servers.
./ovpn user add --username <user>
./ovpn user add --username <user> --expiry 2026-05-01
./ovpn user quota-set --username <user> --monthly-bytes <bytes>
./ovpn user enable --username <user>
./ovpn user expiry-set --username <user> --date 2026-05-01
./ovpn user expiry-clear --username <user>
./ovpn user rm --username <user>

# Read and link commands remain server-scoped.
./ovpn user link --server <server> --username <user>
./ovpn user show --server <server> --username <user>

# Reconcile drift explicitly (dry-run by default).
./ovpn user reconcile --from-server <server> --all
./ovpn user reconcile --from-server <server> --all --apply
```

When `--email` is omitted, new users get stable identity `username@global`.
Legacy users with `username@<old-server>` remain supported and keep working.

Multi-server behavior:

- New servers inherit REALITY cluster parameters by default from the existing baseline.
- New servers are auto-seeded with canonical users at `server add` and re-checked at `init/deploy`.
- Mutating `user` commands (`add|rm|enable|disable|quota-set|quota-reset|expiry-set|expiry-clear`) default to all enabled servers.
- REALITY parameters must match across cluster servers (`private/public key`, `short id`, `server name`, `target`).
- User expiry is cluster-wide and uses UTC end-of-day semantics:
  - `--expiry 2026-05-01` means the user stays active through `2026-05-01 23:59:59 UTC`
  - internally the cutoff is stored as `2026-05-02T00:00:00Z`
  - expired users are effectively disabled on every server even if their manual `enabled` flag remains `true`
  - `expiry-set` to a future date or `expiry-clear` automatically re-enables the user

### 7. Optional monitoring

```bash
# Telegram owner (recommended) and notify targets (optional)
export OVPN_TELEGRAM_OWNER_USER_ID=<owner-user-id>
export OVPN_TELEGRAM_NOTIFY_CHAT_IDS=<chat-id-1>

# Optional: enable owner-only Telegram recovery actions (/restart, /heal)
# export OVPN_TELEGRAM_ADMIN_TOKEN=<long-random-secret>

# Optional custom PDF path inside container (generate locally with `make docs-pdf` if you want the guide bundled)
# export OVPN_TELEGRAM_CLIENTS_PDF_PATH=/opt/ovpn/monitoring/telegram-bot/assets/clients.pdf

# Optional when using custom bot host port:
# export OVPN_TELEGRAM_NOTIFY_URL=http://127.0.0.1:19002/notify

# Place Telegram token on remote host
ssh <ssh-user>@<server-ip> 'install -m 600 /dev/null /opt/ovpn/monitoring/secrets/telegram_bot_token'
ssh <ssh-user>@<server-ip> 'cat > /opt/ovpn/monitoring/secrets/telegram_bot_token'

./ovpn deploy <server>
./ovpn server monitor up <server>
./ovpn server monitor status <server>
```

One-shot automation command:

```bash
OVPN_TELEGRAM_BOT_TOKEN=<token> \
./ovpn server monitor telegram-setup <server> \
  --owner-user-id <owner-user-id>
```

`--owner-user-id` is optional; when omitted, setup uses the first `--notify-chat-ids` value
(or `OVPN_TELEGRAM_NOTIFY_CHAT_IDS`) as owner fallback.

Telegram bot UX is button-first and audit-first:

- Main keyboard: `Home`, `Status`, `Doctor`, `Services`, `Users`, `Traffic`, `Quota`, `Help`
- Inline submenus:
  - Services: `Overview`, per-service drilldowns, `Doctor`, owner recovery buttons (`Heal`, `Restart ...`)
  - Users: `Refresh`, `Top Traffic`, `User link`, `Back`
  - Traffic: `Totals`, `Top 10`, `Today`, `Back`
  - Quota: `Summary`, `Over 80%`, `Over 95%`, `Blocked`, `Back`
- Full `vless://` user link is owner-only (`OVPN_TELEGRAM_OWNER_USER_ID`).
- If `OVPN_TELEGRAM_OWNER_USER_ID` is set, it must be exactly one numeric Telegram user ID.
- Mutating actions (`/restart`, `/heal`) are owner-only and require two-step confirmation.
- Mutating actions are enabled only when `OVPN_TELEGRAM_ADMIN_TOKEN` is configured (deployed as `monitoring/secrets/telegram_admin_token`).
- `User link` uses auto-generated link config from server state (no manual `OVPN_TELEGRAM_LINK_*` required).
- Telegram user identity is globally mirrored by CLI workflows; quota/traffic values shown by bot are local to each server.
- Telegram `/users` and `/doctor` also show expiry state; Alertmanager can notify 2 days before user expiry.
- If `OVPN_TELEGRAM_OWNER_USER_ID` is unset during deploy, CLI now falls back to the first `OVPN_TELEGRAM_NOTIFY_CHAT_IDS` value to prevent bot restart loops.
- `ovpn-telegram-bot` now exposes real `/health` and `/metrics`, restarts itself on stale polling, and keeps alerting alive when optional link config is broken by disabling only `User link`.

### 8. Backups

```bash
./ovpn server backup <server>
ls -lah ~/.ovpn/backups
```

### 9. Decommission old runtime

```bash
./ovpn --dry-run server cleanup <server>
./ovpn server cleanup <server> --confirm CLEANUP
./ovpn server cleanup <server> --confirm CLEANUP --remove-local
```

Cleanup behavior:

- By default, cleanup removes remote runtime and keeps local server metadata, but marks that server as `disabled`.
- Disabled servers are excluded from global user fan-out, REALITY parity checks, and canonical user consistency checks.
- Use `--remove-local` to fully remove server metadata and dependent local rows from `~/.ovpn/ovpn.db`.

## Minimal operational commands

```bash
./ovpn version
./ovpn deploy <server>
./ovpn restart <server>
./ovpn doctor <server>
./ovpn server status <server>
./ovpn server logs <server> --service xray --tail 200
./ovpn stats --server <server>
./ovpn user list --server <server>
./ovpn user add --username <user>
./ovpn user quota-set --username <user> --monthly-bytes <bytes>
./ovpn user reconcile --from-server <server> --all --apply
./ovpn server monitor up <server>
./ovpn server backup <server>
./ovpn server restore <server> --remote-path /opt/ovpn-backups/<archive>.tgz
./ovpn server cleanup <server> --confirm CLEANUP
```

## VPN clients

End-user setup instructions (RU, step-by-step for iOS/Android/Windows/macOS). Generate the optional PDF guide locally with `make docs-pdf` when needed:

- [`docs/clients.md`](docs/clients.md)

## Documentation map

- [`README.md`](README.md): operator entrypoint
- [`README.ansible.md`](README.ansible.md): host bootstrap and hardening
- [`DEVELOPMENT.md`](DEVELOPMENT.md): contributor and architecture guide
- [`docs/security.md`](docs/security.md): security and hardening model
- [`docs/ha.md`](docs/ha.md): HA proxy topology and rollout
- [`docs/monitoring.md`](docs/monitoring.md): monitoring operations
- [`docs/ci.md`](docs/ci.md): GitHub Actions workflows
- [`docs/testing.md`](docs/testing.md): test strategy
- [`docs/upgrades.md`](docs/upgrades.md): upgrades, rollback, cleanup

## Release process

1. Update `VERSION`.
2. Prepend the matching version entry to `CHANGELOG.md`.
3. Merge to `main`.
4. GitHub Actions validates both files, creates the plain semver tag if needed, and publishes the release automatically.

## Protected main

The public repository protects `main` with pull requests and required checks.

Required checks:

- `Go Quality`
- `Ansible Quality`
- `Gitleaks Tree Scan`
- `Trivy FS Scan`

Release tags matching `*.*.*` are protected from update and deletion.

## Support

This repository includes an optional sponsor button and a public donation page:

- sponsor button config: [`.github/FUNDING.yml`](.github/FUNDING.yml)
- donation page source: [`docs/donate/index.html`](docs/donate/index.html)
- default project page URL: `https://agentram.github.io/ovpn/donate/`

Before enabling it publicly:

1. Replace all placeholder wallet addresses.
2. Replace the QR placeholders with real images or image paths.
3. Use project-dedicated donation wallets only.
4. If you later move the page to a custom domain, update [`.github/FUNDING.yml`](.github/FUNDING.yml) to the final URL.

## License

This repository is source-available under the `Attribution-NonCommercial Source License 1.0 (ovpn)`.
You may use, modify, and share it for non-commercial purposes with attribution. Commercial use requires separate permission.
