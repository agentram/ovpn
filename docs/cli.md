# CLI operations guide

This guide is the operator-focused command map for `ovpn`.
It complements the README quick start with practical flows for normal maintenance, user support, and connection diagnostics.

All examples assume the release binary is named `./ovpn`.
For a system-installed binary, replace `./ovpn` with `ovpn`.

## Operating model

- Local desired state lives under `~/.ovpn` by default.
- `server add` changes only local state.
- `server init` and `deploy` render runtime files and apply them to the remote host over SSH/SCP.
- User mutations (`add`, `rm`, `enable`, `disable`, expiry, quota) apply to all enabled VPN servers by default.
- Read/link/diagnostic commands are server-scoped because they inspect one deployed runtime.
- Remote mutating endpoints on `ovpn-agent` require the deployed bearer token; the CLI reads it over SSH from the remote `.env` with `sudo -n`.
- Remote hosts are expected to allow the configured SSH user passwordless `sudo` for `ovpn` operations. If sudo is unavailable, token reads fail and protected runtime calls surface as auth-token mismatches.

Global flags:

```bash
./ovpn --data-dir ~/.ovpn <command>
./ovpn --dry-run <command>
./ovpn --log-level debug <command>
./ovpn --debug <command>
```

Use `--dry-run` for deploy/cleanup previews when available.
Use `--debug` only for operator-side CLI troubleshooting; targeted per-user debug is a different feature and does not enable global debug logging.

## Daily command map

| Task | Command |
| --- | --- |
| Check CLI version | `./ovpn version` |
| Register host | `./ovpn server add ...` |
| First deploy | `./ovpn server init <server>` |
| Normal deploy | `./ovpn deploy <server>` |
| Full health check | `./ovpn doctor <server>` |
| Include recent logs in doctor | `./ovpn doctor <server> --include-logs --tail 100` |
| Compose status | `./ovpn server status <server>` |
| Service logs | `./ovpn server logs <server> --service xray --tail 200` |
| Restart runtime | `./ovpn restart <server>` |
| Backup runtime | `./ovpn server backup <server>` |
| Restore runtime | `./ovpn server restore <server> --remote-path /opt/ovpn-backups/<archive>.tgz` |
| Cleanup runtime | `./ovpn server cleanup <server> --confirm CLEANUP` |

## Server lifecycle

Register a normal VPN host:

```bash
./ovpn server add \
  --name <server> \
  --host <server-ip-or-hostname> \
  --domain <client-domain> \
  --ssh-user root \
  --ssh-port 22 \
  --ssh-identity ~/.ssh/id_rsa \
  --xray-version 26.3.27
```

Useful server flags:

- `--role vpn|proxy`: runtime role, default `vpn`
- `--proxy-preset ru|cn`: routing preset for proxy role
- `--ssh-known-hosts <path>`: known hosts file
- `--ssh-strict-host-key=false`: disable strict host key checking for temporary labs only
- `--reality-server-name`, `--reality-target`: REALITY camouflage target settings
- `--reality-private-key`, `--reality-public-key`, `--reality-short-id`: reuse known REALITY material instead of generating it

First deploy:

```bash
./ovpn server init <server>
./ovpn doctor <server>
./ovpn server status <server>
```

Normal deploy after config/user/runtime changes:

```bash
./ovpn deploy <server>
./ovpn doctor <server>
```

Inspect logs:

```bash
./ovpn server logs <server> --tail 200
./ovpn server logs <server> --service xray --tail 200
./ovpn server logs <server> --service ovpn-agent --tail 200
./ovpn server logs <server> --service ovpn-agent --tail 100 --follow
```

Supported service names include `xray`, `ovpn-agent`, `prometheus`, `alertmanager`, `grafana`, `node-exporter`, `cadvisor`, and `ovpn-telegram-bot`.

Back up before risky work:

```bash
./ovpn server backup <server>
ls -lah ~/.ovpn/backups
```

Cleanup is destructive and requires an explicit token:

```bash
./ovpn --dry-run server cleanup <server>
./ovpn server cleanup <server> --confirm CLEANUP
```

Cleanup defaults are conservative:

- keeps remote backups
- removes ovpn Docker volumes
- includes monitoring cleanup if present
- disables local server metadata
- does not remove local metadata unless `--remove-local` is set

## User lifecycle

Create a user on all enabled VPN servers:

```bash
./ovpn user add --username alice
```

Create with expiry and metadata:

```bash
./ovpn user add \
  --username alice \
  --email alice@global \
  --expiry 2026-12-31 \
  --notes "family phone" \
  --tags family,ios
```

When `--email` is omitted, the stable identity is `username@global`.
The email field is used by Xray stats and diagnostics, so keep it stable across servers.

Show and list users on one server:

```bash
./ovpn user list --server <server>
./ovpn user show --server <server> --username alice
```

Generate the client credential:

```bash
./ovpn user link --server <server> --username alice
./ovpn user link --server <server> --username alice --qr=false
./ovpn user link --server <server> --username alice --qr-file ~/Desktop/alice.png
./ovpn user link --server <server> --username alice --profile vless-reality-xhttp
./ovpn user link --server <server> --username alice --profile vless-reality-xhttp --fingerprint chrome
./ovpn user qr --server <server> --username alice --profile vless-reality-xhttp --out ~/Desktop/alice-xhttp.png
./ovpn user qr --server <server> --username alice --profile vless-reality-xhttp --fingerprint qq --spider-x /assets/alice.js --out ~/Desktop/alice-xhttp-qq.png
./ovpn user link --server <server> --username alice --profile vless-xhttp-plain
./ovpn user link --server <server> --username alice --profile vless-tcp-tls-selfsni-web
./ovpn user export --server <server> --username alice --all-profiles --out ~/Downloads
./ovpn user export --server <server> --username alice --profile vless-reality-xhttp --fingerprints firefox,qq,chrome --out ~/Downloads
```

The `vless://` link and QR code are secrets.
Anyone who has them can use that profile until you disable or remove the user.

New REALITY/TLS links use `fp=firefox` by default. Existing imported client profiles are not changed.
Use `--fingerprint` when you need a specific supported client fingerprint. Use `--spider-x` only for REALITY profiles; if omitted, ovpn generates a stable per-user path.

Transport profiles are server-side opt-in. See [`docs/transports.md`](transports.md) for the profile list, client compatibility notes, and rollout workflow.

Enable, disable, or remove:

```bash
./ovpn user disable --username alice
./ovpn user enable --username alice
./ovpn user rm --username alice
```

Expiry:

```bash
./ovpn user expiry-set --username alice --date 2026-12-31
./ovpn user expiry-clear --username alice
```

Expiry is date-only and remains valid through the end of that UTC day.

Quota:

```bash
./ovpn user quota-set --username alice --monthly-gb 300
./ovpn user quota-set --username alice --monthly-bytes 322122547200
./ovpn user quota-set --username alice --enabled=false
./ovpn user quota-reset --username alice
```

`--monthly-gb` uses `1024^3` bytes per GB.
`quota-reset` clears a quota block and re-adds the user to runtime access.

Top traffic consumers:

```bash
./ovpn user top --server <server>
./ovpn user top --server <server> --limit 20
```

When you add a new enabled VPN server, users are materialized from canonical local state during deploy.
You should not need a separate sync command for normal multi-server operation.

## Transport Profiles

List the profiles known to a server:

```bash
./ovpn server profile list <server>
```

Enable an extra deployable profile and redeploy:

```bash
./ovpn server profile enable <server> vless-reality-xhttp
./ovpn server profile enable <server> vless-xhttp-plain
./ovpn deploy <server>
./ovpn doctor <server>
```

The `vless-tcp-tls-selfsni-web` profile uses `443/tcp`, so it conflicts with `vless-reality-tcp-vision`.
Prepare its certificate/fallback site through Ansible first, then switch the server profile instead of enabling both:

```bash
cd ansible
ANSIBLE_CONFIG=ansible.cfg ansible-playbook -i inventories/production/hosts.yml playbooks/security.yml --limit <server-hostname>
cd ..

./ovpn server profile switch <server> vless-tcp-tls-selfsni-web
./ovpn deploy <server>
./ovpn doctor <server>
curl -vk https://<domain>/
```

The required Ansible variables are:

```yaml
ovpn_camouflage_enabled: true
ovpn_camouflage_domain: vpn-a.example.net
ovpn_camouflage_cert_email: ops@example.net
```

Change the primary profile used by `user link` when `--profile` is omitted:

```bash
./ovpn server profile switch <server> vless-xhttp-plain
./ovpn deploy <server>
```

Plain XHTTP is a fallback profile without Xray REALITY/TLS transport security. It can still be useful on degraded paths, but switch it to primary only deliberately. Already issued links are not changed by `switch`.

Disable a non-primary profile after users have moved away from it:

```bash
./ovpn server profile disable <server> vless-reality-tcp-vision
./ovpn deploy <server>
./ovpn doctor <server>
```

The CLI refuses to disable the current primary profile.
Switch the primary first, deploy, verify users have a working replacement link, and only then disable the old profile:

```bash
./ovpn server profile switch <server> vless-xhttp-plain
./ovpn deploy <server>
./ovpn doctor <server>
./ovpn server profile disable <server> vless-reality-tcp-vision
./ovpn deploy <server>
./ovpn doctor <server>
```

Profile commands change local desired state.
Old links keep working until the next successful deploy removes that profile's inbound from Xray.

`user link`, `user qr`, and `user export --profile` validate that the requested profile exists, is deployable, and is enabled on that server before printing or writing credentials.
If you request a disabled profile, the CLI prints the enable/deploy commands to run instead of emitting a broken link.
Use profile-specific links for A/B testing before switching the primary profile.

## Traffic stats

Show current remote totals from `ovpn-agent`:

```bash
./ovpn stats --server <server>
```

Collect remote totals into local SQLite:

```bash
./ovpn stats sync --server <server>
```

Show daily local traffic:

```bash
./ovpn stats user --server <server> --date 2026-06-06
```

If a local SQLite command briefly fails with `database is locked`, rerun it after the other CLI command finishes.

## Connection diagnostics

Connection diagnostics answer support questions without enabling global debug logs:

- did the server see this user recently?
- are connections accepted or rejected?
- is traffic coming from one approximate network or many?
- are there IPv6 destination attempts?
- which destination ports are most common?
- during a short support window, what destinations did this one user attempt?

Diagnostics are enabled by default with `OVPN_CONNECTION_DIAGNOSTICS=basic`.
That is an intentional support tradeoff: the operator gets per-user connection metadata for troubleshooting, but no payloads, URLs, full source IPs, real UUIDs, or raw lines are stored in SQLite.
Disable before deploy if needed:

```bash
export OVPN_CONNECTION_DIAGNOSTICS=off
./ovpn deploy <server>
```

Default storage model:

- raw Xray access log is scratch data under `/opt/ovpn/logs`
- raw access log is capped at `30 MiB`
- raw access log is not backed up
- SQLite stores aggregates by default
- targeted debug stores only events for active debug sessions

What is stored by default:

- user email and username mapping from local/runtime policy
- last seen timestamp
- accepted/rejected connection counts
- destination family counts (`domain`, `ipv4`, `ipv6`, `unknown`)
- top destination ports
- approximate source network buckets (`IPv4 /24`, `IPv6 /56`) hashed in storage
- IPv6 destination attempt counts

What is not stored by default:

- payloads
- URLs
- full source IPs
- real UUIDs in diagnostics tables
- raw access-log lines in SQLite

Approximate source networks are not exact devices.
One phone moving between Wi-Fi and LTE may appear as multiple source networks, and several devices behind one NAT may appear as one source network.
For exact accountability, issue separate users/profiles per person or device.

### Quick diagnose

Use `diagnose` first:

```bash
./ovpn user diagnose --server <server> --username alice --since 24h
```

JSON output is useful for scripts or attaching evidence to an issue:

```bash
./ovpn user diagnose --server <server> --username alice --since 24h --json
```

Read the output as:

- `enabled/effective/expired/quota_blocked`: local/runtime access state
- `quota`: rolling 30d usage versus limit
- `traffic`: 1h/6h/24h/30d usage from existing stats
- `last_seen`: whether the server has recently seen this user
- `accepted/rejected`: whether Xray accepted connections for this user
- `approx_source_networks`: rough signal for shared profiles or network churn
- `top_ports`: common destination ports, usually `443` for HTTPS-like traffic
- `ipv6_destinations`: a hint when client apps prefer IPv6 destinations

If `last_seen` is empty and counts are zero, the server probably does not see the profile at all.
Check the selected profile, link/QR, domain, client network, and server reachability.

If accepted counts increase but one app does not work, the VPN profile is reaching the server.
Move to targeted debug for that user and ask them to reproduce the app issue.

### Targeted debug workflow

Start a short debug session:

```bash
./ovpn user debug start --server <server> --username alice --duration 15m
```

List active sessions:

```bash
./ovpn user debug list --server <server>
./ovpn user debug list --server <server> --json
```

Ask the user to reproduce the issue during the window, then inspect events:

```bash
./ovpn user debug show --server <server> --username alice --since 15m
./ovpn user debug show --server <server> --username alice --since 15m --json
```

Stop early when finished:

```bash
./ovpn user debug stop --server <server> --username alice
```

Duration defaults to `15m` and is capped at `24h`.
Use the shortest useful window.
Stopping a debug session removes that user's captured debug events immediately.
The aggregate counters remain available for normal diagnostics.

Targeted debug captures:

- timestamp
- result (`accepted` or `rejected`)
- user email
- masked source network
- destination host/IP when present in the Xray access log
- destination port
- destination family

It does not enable global Xray debug logging and does not capture payloads.

### Routing and IPv6 notes

Rendered Xray routing uses `domainStrategy=AsIs`.
This keeps domain destinations visible to routing and diagnostics instead of resolving every domain during route matching.

For normal `vpn` servers this mainly affects diagnostics readability.
For `proxy` servers it means:

- explicit `geosite` and domain-suffix direct rules still match domain destinations
- `geoip:*` direct rules match IP-literal destinations
- domain destinations not matched by the preset's domain rules fall through to the foreign pool

The freedom outbound prefers IPv4 for domain resolution.
IPv6-literal destinations are not blocked by default because mobile clients may try IPv6 first and otherwise wait for timeout before IPv4 fallback.

### Practical support flow

When a user says "the VPN is connected but nothing opens":

```bash
./ovpn doctor <server>
./ovpn server logs <server> --service xray --tail 100
./ovpn user diagnose --server <server> --username <user> --since 24h
```

If the user is not seen:

- confirm the selected client profile
- generate a fresh QR/link if the profile may be wrong
- ask whether the issue happens on Wi-Fi, mobile network, or both
- check whether other users from the same region/operator are affected

If the user is seen and accepted:

```bash
./ovpn user debug start --server <server> --username <user> --duration 15m
./ovpn user debug list --server <server>
# ask the user to open the failing app for 2-3 minutes
./ovpn user debug show --server <server> --username <user> --since 15m
./ovpn user debug stop --server <server> --username <user>
```

If many approximate source networks appear for one user, treat that as a shared-profile signal, not proof of exact device count.
The operational fix is to create separate users/profiles and retire the shared profile after migration.

## Monitoring commands

Start/stop the optional monitoring stack:

```bash
./ovpn server monitor up <server>
./ovpn server monitor status <server>
./ovpn server monitor down <server>
```

Telegram setup:

```bash
OVPN_TELEGRAM_BOT_TOKEN=<token> \
./ovpn server monitor telegram-setup <server> \
  --owner-user-id <owner-user-id>
```

Useful Telegram setup flags:

- `--token <token>` or `OVPN_TELEGRAM_BOT_TOKEN`
- `--notify-chat-ids <ids>`
- `--owner-user-id <id>`
- `--monitor-up=false`
- `--test-notify=false`

The Telegram bot is read-only by default.
Owner-only recovery actions require `OVPN_TELEGRAM_ADMIN_TOKEN`.

See [`monitoring.md`](monitoring.md) for dashboards, alerts, and Telegram behavior.

## HA proxy commands

Register a proxy:

```bash
./ovpn server add \
  --name <proxy> \
  --role proxy \
  --proxy-preset ru \
  --host <proxy-ip> \
  --domain <proxy-domain> \
  --ssh-user root \
  --ssh-port 22
```

Attach and inspect backends:

```bash
./ovpn server backend attach --proxy <proxy> --backend <vpn-server> --priority 100
./ovpn server backend list --proxy <proxy>
./ovpn server backend detach --proxy <proxy> --backend <vpn-server>
```

After changing proxy backend attachments, deploy the affected backend and proxy:

```bash
./ovpn deploy <vpn-server>
./ovpn deploy <proxy>
./ovpn doctor <proxy>
```

See [`ha.md`](ha.md) for the full proxy model.

## Config validation

Render Xray JSON for inspection:

```bash
./ovpn config render --server <server>
```

Validate rendered config locally and, when Docker is available, with the pinned Xray image:

```bash
./ovpn config validate --server <server>
```

Use config validation before a risky deploy or when changing Xray-related defaults.

## Advanced local-state repair

`user reconcile` exists as a hidden repair command for unusual local-state drift.
It is not part of normal server onboarding and should not be used as a routine sync step.

Normal behavior:

- user mutations apply to all enabled VPN servers
- deploy materializes canonical users onto a newly added server
- link/read/diagnostic commands remain server-scoped

Use reconcile only if `~/.ovpn` has drifted after manual SQLite edits, an old migration, or a failed repair and you have already inspected the local state.
It copies local user rows from one server record to another server record, is dry-run by default, and does not change the live remote runtime until you deploy the affected server.

```bash
./ovpn user reconcile --from-server <known-good-server> --to-server <target-server>
./ovpn user reconcile --from-server <known-good-server> --to-server <target-server> --apply
./ovpn deploy <target-server>
```

Prefer normal commands whenever possible:

```bash
./ovpn user add --username <user>
./ovpn user disable --username <user>
./ovpn deploy <server>
```

## Safe troubleshooting checklist

For a newly bootstrapped host:

```bash
./ovpn server status <server>
./ovpn doctor <server>
./ovpn server logs <server> --service ovpn-agent --tail 100
./ovpn server logs <server> --service xray --tail 100
```

For a user who cannot connect:

```bash
./ovpn user show --server <server> --username <user>
./ovpn user diagnose --server <server> --username <user> --since 24h
./ovpn user link --server <server> --username <user> --qr-file ~/Desktop/<user>.png
```

For high traffic:

```bash
./ovpn user top --server <server> --limit 20
./ovpn user diagnose --server <server> --username <user> --since 24h
./ovpn user quota-set --username <user> --monthly-gb <gb>
```

For a shared profile:

```bash
./ovpn user add --username <person-or-device>
./ovpn user link --server <server> --username <person-or-device> --qr-file ~/Desktop/<person-or-device>.png
./ovpn user disable --username <old-shared-profile>
```

Do not leave targeted debug running longer than needed.
Use `debug list` during support and `debug stop` when finished.
