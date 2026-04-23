# High Availability Proxy Topology

This document describes the additive HA extension for `ovpn`.

## Goal

Add a preset-driven `proxy` entrypoint in front of existing foreign `vpn` servers without breaking current direct users.
The first built-in preset is Russia (`ru`).

Resulting behavior:

- current direct users on existing `vpn` servers keep working unchanged
- new or migrated users can receive one proxy link
- destinations matched by the active proxy preset exit directly from the proxy
- all other destinations are relayed through foreign backend `vpn` servers
- backend failure is handled by local HAProxy failover on the proxy node

## What the proxy host is

The proxy host is a normal `ovpn` server record with:

- `role=proxy`
- `proxy_preset=<country-preset>`
- one public domain for client entry
- one or more attached backend `vpn` servers

The proxy host does not replace existing backends.
It is an extra entrypoint layered on top of them.

## How the proxy host is bootstrapped

Host bootstrap remains the same as for regular VPN nodes.

Ansible owns only:

- base OS preparation
- Docker / Compose
- SSH hardening
- firewall / fail2ban / host security

`ovpn` owns:

- runtime config rendering
- Xray config
- HAProxy config
- monitoring bundle
- deploy / doctor / user state

Recommended bootstrap order:

1. Add the proxy host to Ansible inventory under `proxy_servers`.
2. Run Ansible bootstrap and security playbooks.
3. Register the host in local `ovpn` state with `--role proxy --proxy-preset <preset>`.
4. Initialize and deploy it with `ovpn`.
5. Attach foreign backends.
6. Redeploy and validate.

## What runs on the proxy host

After `ovpn deploy <proxy>`, the proxy host runs:

- `xray`
  - public client inbound on `443/tcp`
  - split routing logic
  - direct egress for preset-local routes
  - `foreign-pool` relay outbound for all non-local routes
- `haproxy`
  - local TCP failover layer for foreign backends
  - Prometheus metrics on `:8404/metrics`
- `ovpn-agent`
  - runtime/API health and local metrics
- optional monitoring stack:
  - `prometheus`
  - `alertmanager`
  - `grafana`
  - `node-exporter`
  - `cadvisor`
  - `ovpn-telegram-bot`

Additional proxy-only runtime files:

- `/opt/ovpn/haproxy/haproxy.cfg`
- `/opt/ovpn/geodata/geosite.dat`
- `/opt/ovpn/geodata/geoip.dat`

## Traffic flow

### Preset-local traffic

Client -> proxy Xray -> `direct`

Traffic matched by the active proxy preset does not traverse foreign backends.

### Foreign traffic

Client -> proxy Xray -> `foreign-pool` -> local HAProxy -> selected foreign backend -> remote internet

If one backend fails, HAProxy routes new connections to another healthy backend.

If all foreign backends fail, foreign traffic fails closed.
It is not sent directly from the proxy country.

## Proxy presets

Proxy behavior is selected by `proxy_preset`.
If it is omitted, `ovpn` defaults proxy nodes to `ru` for backward compatibility with the first HA implementation.

Current built-in preset:

- `ru`
  - direct domains: `geosite:ru-available-only-inside`, `.ru`, `.su`, `.xn--p1ai`
  - direct IPs: `geoip:ru`, `geoip:private`
  - geodata defaults: runetfreedom geosite/geoip feeds

Future presets can be added without changing the `proxy` role model itself.

## How the current `ru` preset determines local destinations

The proxy uses Xray split-routing rules in this order:

- `geosite:ru-available-only-inside`
- domain suffix matches for `.ru`, `.su`, and `.xn--p1ai`
- `geoip:ru`
- `geoip:private`

Anything matched by those rules is sent to `direct` on the proxy.
All remaining user traffic is sent to `foreign-pool`, which is the local HAProxy-backed relay to foreign VPN servers.

This means routing is based on a mix of:

- Xray geosite data for known Russia-only services
- explicit Russian domain suffixes
- Russian IP geo matches
- local/private address ranges

If a destination does not match those rules, it is treated as foreign.

## Existing users and compatibility

Existing users continue working because:

- current `vpn` runtime path is unchanged
- `proxy` is additive and opt-in
- existing user links remain valid
- existing `vpn` servers are not converted into proxy nodes

Operationally this means:

- no disruption for users who stay on direct backend links
- HA becomes available only for users moved to the proxy entrypoint
- old links remain the rollback path during migration

## Step-by-step rollout

1. Prepare the proxy host with Ansible:

```bash
cd ansible
ANSIBLE_CONFIG=ansible.cfg ansible-playbook -i inventories/production/hosts.yml playbooks/bootstrap.yml --limit <proxy-host>
ANSIBLE_CONFIG=ansible.cfg ansible-playbook -i inventories/production/hosts.yml playbooks/security.yml --limit <proxy-host>
```

2. Register the proxy in local state:

```bash
./ovpn server add \
  --name proxy-ru \
  --role proxy \
  --proxy-preset ru \
  --host <proxy-ip> \
  --domain <proxy-domain> \
  --ssh-user root \
  --ssh-port 22 \
  --reality-private-key <private-key> \
  --reality-public-key <public-key> \
  --reality-short-id <short-id>
```

3. Initialize it:

```bash
./ovpn server init proxy-ru
```

4. Attach one backend first:

```bash
./ovpn server backend attach --proxy proxy-ru --backend <vpn-backend-1>
```

The first attach lazily provisions the shared backend relay identity if the backend does not have one yet.

5. Deploy the attached backend first:

```bash
./ovpn config validate --server <vpn-backend-1>
./ovpn deploy <vpn-backend-1>
./ovpn doctor <vpn-backend-1>
```

6. Validate and deploy the proxy:

```bash
./ovpn config validate --server proxy-ru
./ovpn deploy proxy-ru
./ovpn doctor proxy-ru
```

7. Start monitoring:

```bash
./ovpn server monitor up proxy-ru
./ovpn server monitor status proxy-ru
```

If a fresh host hits public registry pull limits, preload the monitoring images from another ovpn host or redeploy with explicit image overrides before starting monitoring.
The HA design does not require Docker Hub specifically; it only requires that the configured image references are reachable on the proxy host.

8. Verify:

- `haproxy` is running
- Prometheus scrapes `haproxy`
- Grafana shows `ovpn Proxy HA Overview`
- Telegram bot shows `haproxy` in Services on the proxy
- preset-local routes exit directly
- foreign routes use the attached backend

9. Attach additional backends one by one:

```bash
./ovpn server backend attach --proxy proxy-ru --backend <vpn-backend-2>
./ovpn deploy <vpn-backend-2>
./ovpn deploy proxy-ru
./ovpn doctor proxy-ru
```

10. Pilot proxy links with a small user set.

11. Migrate existing users in batches only after the proxy path is stable.

## Monitoring expectations on the proxy

Proxy monitoring now adds:

- HAProxy scrape target in Prometheus
- backend pool alerts
- HAProxy container alerts
- proxy dashboard: `ovpn Proxy HA Overview`
- Telegram bot HAProxy service visibility and restart action

## Failure model

This phase covers backend HA only.

The proxy host itself is still a single point of failure.

If the proxy host dies:

- direct users on existing backends still work
- proxy users lose service until the proxy host is restored

## Troubleshooting

### Client connects but sites load forever

This usually means the client-to-proxy leg is working, but the proxy-to-backend leg is broken.

Check in this order:

1. Proxy Xray logs:

```bash
./ovpn server logs proxy-ru --service xray --tail 200
```

Expected: `vless-reality -> foreign-pool` for the affected user.

2. HAProxy counters on the proxy:

```bash
./ovpn server logs proxy-ru --service haproxy --tail 200
```

And in Grafana / Prometheus:

- `haproxy_backend_sessions_total`
- `haproxy_server_sessions_total`

Expected: counters increase while the user retries traffic.

3. Backend Xray logs:

```bash
./ovpn server logs <vpn-backend> --service xray --tail 200
```

Expected: traffic from the proxy is accepted by `proxy-service@cluster`.

If the backend logs `invalid request user id`, the backend was not redeployed with the current proxy service identity.
Redeploy the backend first, then redeploy the proxy if needed.

4. Backend doctor check:

```bash
./ovpn doctor <vpn-backend>
```

The backend doctor now verifies that the live runtime config contains `proxy-service@cluster` when that backend is attached to a proxy.

## Commands you will use most

```bash
./ovpn server backend list --proxy proxy-ru
./ovpn config validate --server proxy-ru
./ovpn deploy proxy-ru
./ovpn doctor proxy-ru --include-logs
./ovpn server logs proxy-ru --service haproxy --tail 200
./ovpn server monitor status proxy-ru
```
