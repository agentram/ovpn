# Security model

This document defines the practical security posture for `ovpn`.

## Scope

Recommended host model:

- Xray (`VLESS + REALITY`) on `443/tcp`
- Optional XHTTP fallback profile on `13179/tcp`
- Optional VLESS Encryption + XHTTP profile on `13180/tcp`
- Optional self-SNI HTTPS camouflage profile on `443/tcp`, with a real certificate and an internal static fallback site
- SSH control plane on `22/tcp`
- Ansible for host baseline and hardening
- `ovpn` for runtime lifecycle
- No external reverse-proxy or certificate-management layer is required in the recommended REALITY flow

## Threat-surface baseline

Public surface should stay minimal:

- Xray transport port (`443/tcp`)
- Optional HTTP challenge port (`80/tcp`) when self-SNI certificate issuance is enabled
- Optional XHTTP fallback port (`13179/tcp`) when that profile is enabled
- Optional VLESS Encryption + XHTTP port (`13180/tcp`) when that profile is enabled
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
- Treat `fp` and `spiderX` as client-link hardening knobs; they do not require server-side target rotation.

Operational rules:

- Do not use wildcard-style server names.
- Avoid placeholder `reality_target` values.
- Keep `reality_server_name` aligned with a certificate name served by `reality_target`.
- Rotate `reality_target` / `serverNames` only as an operator change: update config, deploy, then issue new REALITY links.
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
- IPv6-literal destinations (`::/0`) -> `outboundTag: "block"` so IPv4-only VPS hosts fail fast instead of timing out on client-preferred IPv6 targets

Rendered routing uses `domainStrategy=AsIs`, so IP-literal destinations remain visible to routing and diagnostics.
The default freedom outbound prefers IPv4 for domain resolution, and IPv6-literal destinations are blocked by default in the minimal profile.
This is intentional for small IPv4-only VPS hosts where mobile clients may otherwise connect successfully but hang on IPv6 targets that the host cannot route.

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

## Optional self-SNI HTTPS fallback

The `vless-tcp-tls-selfsni-web` profile is an explicit operator choice for hosts where a normal HTTPS response on the VPN domain is useful.
It differs from REALITY:

- REALITY uses an external `reality_target` for failed-auth behavior.
- self-SNI uses a real certificate for the server domain and Xray's VLESS TCP/TLS fallback.
- Xray remains the only public listener on `443/tcp`.
- The fallback web service is an internal Docker sidecar (`ovpn-web`) exposed only inside the Compose network.

When `ovpn_camouflage_enabled: true`, the Ansible security role includes the separate `camouflage.yml` task file and prepares:

- `/opt/ovpn/certs` for the runtime certificate/key copy
- `/opt/ovpn/camouflage-site` for a boring static site
- certbot issuance/renewal for the configured domain
- a renewal deploy hook that refreshes the runtime cert files and recreates the Xray container
- `80/tcp` firewall access for HTTP-01 certificate validation

Set these values in `host_vars/<server-hostname>.yml` before switching a server to `vless-tcp-tls-selfsni-web`:

```yaml
ovpn_camouflage_enabled: true
ovpn_camouflage_domain: vpn-a.example.net
ovpn_camouflage_cert_email: ops@example.net
```

Set `ovpn_camouflage_cert_email` in production inventory when possible. Without it, certbot can register the certificate without an email address, but Let's Encrypt cannot send expiry or renewal-failure notices.

The `80/tcp` rule stays open while camouflage is enabled because unattended `certbot renew --standalone` needs the HTTP-01 challenge port later, not only during first issuance. Nothing in ovpn listens on `80/tcp` between renewals.

If `ovpn_manage_firewall: false`, Ansible does not manage host firewall rules.
In that case, open `80/tcp` through your separate firewall process before certificate issuance.

Keep the fallback site ordinary. Do not put VPN branding, operator notes, hidden diagnostics, tokens, or user-specific content on it.
The goal is a normal HTTPS response for accidental traffic and simple probes, not a public control surface.

The profile conflicts with `vless-reality-tcp-vision` because both use `443/tcp`.
Ansible only prepares certificates, firewall access, and the fallback site; it does not enable the Xray profile by itself. Use `server profile switch`, then deploy and verify:

```bash
./ovpn server profile switch <server> vless-tcp-tls-selfsni-web
./ovpn deploy <server>
./ovpn doctor <server>
curl -vk https://<domain>/
```

Users need new links only when they switch to this profile.

## Optional VLESS Encryption + XHTTP

`vless-xhttp-vlessenc` is an experimental XHTTP profile on `13180/tcp`. It uses Xray's VLESS Encryption key agreement and replay protection, but it is not a replacement for testing the transport against the client networks that matter to you.

Before enabling it, allow `13180/tcp` through the host firewall using the same Ansible inventory used for the host baseline:

```yaml
ovpn_firewall_extra_tcp_ports:
  - 13180
```

Then apply host maintenance, enable the profile, deploy, and validate:

```bash
cd ansible
ANSIBLE_CONFIG=ansible.cfg ansible-playbook -i inventories/production/hosts.yml playbooks/host-maintenance.yml --limit <server-hostname>
cd ..

./ovpn server profile enable <server> vless-xhttp-vlessenc
./ovpn deploy <server>
./ovpn doctor <server>
```

The generated client link contains an `encryption` value. The matching server `decryption` value is kept encrypted in local ovpn state and must never be sent to users, logged, or pasted into a client. The current confirmed client is Mihomo; treat other clients as unconfirmed until they have passed an import and traffic test. Do not combine this VLESS Encryption setting with the self-SNI profile because its TLS fallback path requires a normal VLESS inbound without `decryption`.

## SSH and host hardening defaults

- Keep SSH on `22` in the recommended flow.
- Prefer key-only auth.
- Keep root access policy explicit and controlled by inventory.
- Use fail2ban/UFW through Ansible policy, not ad-hoc commands.
- Recommended host is clean ovpn-only; Ansible should not remove unrelated services unless explicitly declared in inventory.
- SSH agent forwarding is disabled by default (`ovpn_ssh_allow_agent_forwarding: false`).
- Obsolete public firewall allows can be removed with `ovpn_firewall_remove_tcp_ports`.
- Explicitly declared apt source files and packages can be removed with `ovpn_remove_apt_source_files` and `ovpn_purge_packages`; keep these lists empty unless a host needs cleanup.
- Existing runtime secrets and backup archives are locked down when present; missing files are ignored so fresh hosts still bootstrap cleanly. The Xray config is installed as `root:<xray-gid> 0640`, so the Xray container can read it without making the REALITY private key and client UUIDs world-readable.
- Docker daemon defaults enable live-restore and json-file log rotation.
- Docker daemon defaults are merged into existing `/etc/docker/daemon.json` content so unrelated daemon settings are preserved.
- The optional OVPN MOTD summarizes host role, domain, deploy root, VPN port, monitoring tunnel policy, and the no-auto-reboot policy.
- Journald limits are enforced by default (`ovpn_journald_system_max_use=200M`, `ovpn_journald_runtime_max_use=100M`).
- Swapfile is enabled by default (`ovpn_enable_swapfile: true`, `ovpn_swapfile_size_mb: 2048`, `ovpn_swapfile_swappiness: 10`). Host maintenance resizes an existing swapfile to the configured size; when growing an active swapfile it uses a temporary swapfile first so memory pressure does not make `swapoff` fail.
- Conntrack is tuned for VPN/NAT workloads by default (`ovpn_conntrack_max=65536`, `ovpn_conntrack_tcp_timeout_established=86400`) to avoid packet drops when many short-lived connections are active. The security role also loads `nf_conntrack` during boot so these sysctl values are applied reliably after restarts. A host-side timer writes conntrack usage into node-exporter's textfile collector every 60 seconds.

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
- Monitoring defaults are tuned for small 1GB-class hosts:
  - Prometheus scrape/evaluation interval `60s`
  - Prometheus TSDB retention `10d` with a `512MB` size cap
  - Prometheus WAL compression enabled
  - cAdvisor housekeeping `60s`, max `5m`
  - Critical host memory guard at `MemAvailable < 64MiB` for `2m`
  - Container OOM events alerted from cAdvisor
  - Grafana background reporting and update checks disabled
  - Warning memory/collector alerts use longer windows before firing, but still send resolved Telegram notifications.

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
- Bot may send operator guide PDFs via `sendDocument`:
  - `OVPN_TELEGRAM_CLIENTS_PDF_PATH` (English, default generated `clients.pdf`)
  - `OVPN_TELEGRAM_CLIENTS_RU_PDF_PATH` (Russian, default generated `clients-ru.pdf`)
  - build both with `make docs-pdf` before deploy
- Never log or return token/private keys/password-like values.

## Secrets and state handling

- Local DB (`~/.ovpn/ovpn.db`) contains sensitive metadata.
- Remote runtime (`/opt/ovpn`) contains runtime secrets/config.
- Backups may contain secrets and must be treated as sensitive.
- Deploy keeps `/opt/ovpn/.env` at `0600` for the deploying account, keeps Xray config at `root:<xray-gid> 0640`, and treats remote backups/snapshots as sensitive because they may contain runtime secrets.
- Keep inventory secrets in `ansible-vault`.
- Logs redact `vless://` links and common secret-like inline values (`password`, `token`, `private_key`), but avoid logging secrets intentionally.

## Quota enforcement model

- Quota window is rolling last `30d`.
- Default quota is `300 GB` when per-user limit is unset and quota is enabled.
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
