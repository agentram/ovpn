# Security model

This document defines the practical security posture for `ovpn`.

## Scope

Recommended production model:

- Xray (`VLESS + REALITY`) on `443/tcp`
- SSH control plane on `22/tcp`
- Ansible for host baseline and hardening
- `ovpn` for runtime lifecycle
- No external reverse-proxy or certificate-management layer is required in the recommended flow

## Threat-surface baseline

Public surface should stay minimal:

- Xray transport port (`443/tcp`)
- SSH (`22/tcp`)

Internal-only surfaces:

- Xray API
- `ovpn-agent` HTTP endpoint
- Monitoring internals unless explicitly tunneled
- Alertmanager -> `ovpn-telegram-bot` webhook path (internal Docker network)

## REALITY hardening guidance

Follow official Xray guidance:

- Use strict, explicit `serverNames`.
- Use a realistic, stable `reality_target`.
- Treat fallback as anti-active-probing/shared-port behavior.
- Understand failed auth traffic is forwarded to `target`.

Operational rules:

- Do not use wildcard-style server names.
- Avoid sloppy or placeholder `reality_target` values.
- Keep `OVPN_SECURITY_PROFILE=minimal` unless you need emergency rollback.
- Minimal profile adds protocol/domain blocking and threat DNS resolvers.
- Keep fallback rate limits disabled by default.
- Enable fallback limits only for explicit abuse control.

Minimal profile environment controls:

- `OVPN_SECURITY_PROFILE=minimal|off` (default `minimal`)
- `OVPN_THREAT_DNS_SERVERS` (default `9.9.9.9,149.112.112.112`)

Minimal profile runtime controls include:

- `protocol: ["bittorrent"] -> outboundTag: "block"`
- `domain: ["geosite:category-public-tracker"] -> outboundTag: "block"`
- Xray `dns.servers` with threat resolvers

If Xray image validation fails because geosite resources are missing:

```bash
export OVPN_SECURITY_PROFILE=off
./ovpn deploy <server>
```

Optional fallback rate-limit env settings:

- `OVPN_REALITY_LIMIT_FALLBACK_UPLOAD_AFTER_BYTES`
- `OVPN_REALITY_LIMIT_FALLBACK_UPLOAD_BYTES_PER_SEC`
- `OVPN_REALITY_LIMIT_FALLBACK_UPLOAD_BURST_BYTES_PER_SEC`
- `OVPN_REALITY_LIMIT_FALLBACK_DOWNLOAD_AFTER_BYTES`
- `OVPN_REALITY_LIMIT_FALLBACK_DOWNLOAD_BYTES_PER_SEC`
- `OVPN_REALITY_LIMIT_FALLBACK_DOWNLOAD_BURST_BYTES_PER_SEC`

Note: Xray docs warn fallback rate limits may be fingerprintable. Use intentionally.

## SSH and host hardening defaults

- Keep SSH on `22` in the recommended flow.
- Prefer key-only auth.
- Keep root access policy explicit and controlled by inventory.
- Use fail2ban/UFW through Ansible policy, not ad-hoc commands.
- Recommended host is clean ovpn-only; Ansible should not remove unrelated services unless explicitly declared in inventory.
- SSH agent forwarding is disabled by default (`ovpn_ssh_allow_agent_forwarding: false`).
- Obsolete public firewall allows can be removed with `ovpn_firewall_remove_tcp_ports`.
- Explicitly declared apt source files and packages can be removed with `ovpn_remove_apt_source_files` and `ovpn_purge_packages`; keep these lists empty unless a host needs cleanup.
- Existing runtime secrets and backup archives are locked down when present; missing files are ignored so fresh hosts still bootstrap cleanly. The Xray config keeps portable read permissions so container image UID changes do not break startup.
- Docker daemon defaults enable live-restore and json-file log rotation.
- Docker daemon defaults are merged into existing `/etc/docker/daemon.json` content so unrelated daemon settings are preserved.
- The optional OVPN MOTD summarizes host role, domain, deploy root, VPN port, monitoring tunnel policy, and the no-auto-reboot policy.
- Journald limits are enforced by default (`ovpn_journald_system_max_use=200M`, `ovpn_journald_runtime_max_use=100M`).
- Swapfile is enabled by default (`ovpn_enable_swapfile: true`, `ovpn_swapfile_size_mb: 1024`, `ovpn_swapfile_swappiness: 10`).

Use `playbooks/bootstrap.yml` for fresh hosts. Use `playbooks/host-maintenance.yml` for already-deployed hosts when you need to apply host baseline changes without rewriting `/opt/ovpn` runtime scaffolding.

## Unattended upgrades policy

Ansible enables unattended upgrades with a conservative host-maintenance policy:

- Ubuntu security and ESM security updates are automatic.
- Normal Ubuntu `-updates` are manual unless `ovpn_unattended_enable_ubuntu_updates=true`.
- Docker repository packages remain manual maintenance because upgrades may restart container runtime components.
- Automatic reboot is disabled unless `ovpn_unattended_auto_reboot=true`.
- Host-specific package blacklists can be set with `ovpn_unattended_package_blacklist`.

Defaults:

```yaml
ovpn_enable_unattended_upgrades: true
ovpn_unattended_enable_ubuntu_updates: false
ovpn_unattended_auto_reboot: false
ovpn_unattended_auto_reboot_with_users: false
ovpn_unattended_auto_reboot_time: "03:30"
ovpn_unattended_package_blacklist: []
```

Pending MOTD updates can still appear for normal Ubuntu updates, Docker packages, held packages, or a reboot required by an already-installed kernel. Treat those as manual maintenance signals, not as unattended-upgrades failure.

Ansible does not reboot hosts by default. Reboots remain an explicit operator maintenance action.

## Optional Tor exit-node host profile

Ansible can optionally block Tor exit IPs at host level for `443/tcp` only.

Defaults:

- `ovpn_block_tor_exit_nodes: false`
- `ovpn_tor_exit_block_port: 443`
- `ovpn_tor_exit_list_url: https://check.torproject.org/torbulkexitlist`
- `ovpn_tor_update_schedule: daily`

Implementation behavior:

- daily systemd timer refreshes list
- atomic update through `ipset` temp set + `swap`
- fail-open: fetch/parse errors do not wipe existing set/rules
- scope is limited to INPUT `tcp/443` to avoid collateral impact on other ports/services

Disable path removes timer/service/rule/set.

## Loopback conflict policy

`ovpn-agent` binds loopback host port `19000` by default.

If occupied, deploy with a different loopback port:

```bash
export OVPN_AGENT_HOST_PORT=19001
./ovpn deploy <server>
```

`ovpn-telegram-bot` binds loopback host port `19001` by default for local event relay.
If occupied, set a different value:

```bash
export OVPN_TELEGRAM_BOT_HOST_PORT=19002
./ovpn deploy <server>
```

## Capacity and retention defaults

- Remote server backup archives: keep latest `7`.
- Local backup archives: keep latest `7`.
- Remote pre-deploy snapshots (`ovpn-*`): keep latest `7`.
- Monitoring defaults are tuned for 1GB-class VPS:
  - Prometheus scrape/evaluation interval `30s`
  - Prometheus TSDB retention `10d`
  - cAdvisor housekeeping `30s`, max `2m`

## Telegram bot boundaries

- Use file-backed secret token (`monitoring/secrets/telegram_bot_token`).
- Restrict operator access with owner-only policy (`OVPN_TELEGRAM_OWNER_USER_ID`).
- Keep Telegram UX read-only (menu/buttons + slash fallback).
- Bot must not expose write/admin actions or shell execution.
- Full VLESS links are owner-only:
  - `OVPN_TELEGRAM_OWNER_USER_ID`
  - value must be exactly one numeric Telegram user ID
  - non-owner users receive deny response for `User link`
- Link generation config is auto-generated by deploy into `monitoring/telegram-bot/link-config.json`.
- Bot may send operator guide PDF via `sendDocument`:
  - `OVPN_TELEGRAM_CLIENTS_PDF_PATH` (default generated `clients.pdf`; build with `make docs-pdf` before deploy)
- Never log or return token/private keys/password-like values.

## Secrets and state handling

- Local DB (`~/.ovpn/ovpn.db`) contains sensitive metadata.
- Remote runtime (`/opt/ovpn`) contains runtime secrets/config.
- Backups may contain secrets and must be treated as sensitive.
- Deploy and host maintenance keep `/opt/ovpn/.env` at `root:root 0600`, Xray config at `root:root 0644`, and backup archives at `root:root 0600`.
- Keep inventory secrets in `ansible-vault`.
- Logs redact `vless://` links and common secret-like inline values (`password`, `token`, `private_key`), but avoid logging secrets intentionally.

## Quota enforcement model

- Quota window is rolling last `30d`.
- Default quota is `200 GB` when per-user limit is unset and quota is enabled.
- User is blocked when `usage >= quota` and unblocked automatically when usage drops below quota window threshold.
- Per-user speed limiting is intentionally out of scope in this stage.

## Security validation checklist

```bash
./ovpn doctor <server>
./ovpn server status <server>
./ovpn server logs <server> --service xray --tail 200
./ovpn server logs <server> --service ovpn-agent --tail 200
```

## Official references

- Xray transport: https://xtls.github.io/en/config/transport.html
- Xray fallback: https://xtls.github.io/en/config/features/fallback.html
- Xray inbound: https://xtls.github.io/en/config/inbound.html
