# OVPN Desk development build

The development build reads existing server/user inventory, runs explicit native HTTPS checks and supports opt-in automatic observation and Checker/Knife jobs. Users shows configured assignments, policy differences, quota limits and expiry without exposing credentials. It uses Wails 3, React/TypeScript and a bundled OVPN worker. Server deployment, user mutations, subscription management and recovery actions remain in the CLI.

English is the default. Settings offers Russian and dark/light themes; only these preferences are persisted in WebView storage. The application opens the existing local workspace on startup. No database is created or migrated by desktop queries.

## Build and open

Requirements: the repository's Go toolchain, Node.js 26/npm and Python 3. macOS native builds require Xcode Command Line Tools. Other native build prerequisites follow [Wails installation](https://v3.wails.io/quick-start/installation/).

From the repository root:

```bash
python3 scripts/build_desktop.py
```

On macOS:

```bash
open "bin/desktop/darwin/OVPN Desk.app"
```

The macOS development bundle is ad-hoc signed and targets macOS 13+. It is not notarized or a public distribution build. To open an explicitly selected workspace:

```bash
"bin/desktop/darwin/OVPN Desk.app/Contents/MacOS/ovpn-desk" \
  --data-dir /path/to/workspace
```

A Windows x64 cross-build is available:

```bash
python3 scripts/build_desktop.py --target windows --arch amd64
```

This produces an executable and matching worker, not an installer. Native Windows/WebView2, Linux/GTK, SSH-agent and credential-vault integration remain validation gates. A cross-build alone does not establish platform support. Keep the worker beside the application executable; its digest is pinned into the host at build time. Rebuild the host after changing the worker. Distribution signing must sign the worker **before** calculating its digest.

Use the native application to exercise the workspace and check controls. Production assets are bundled with the application.

## Users and observation

Open **Users** in Local workspace to inspect configured access. Policy states are local evidence: Allowed does not establish runtime distribution or user connectivity. The last manual check is linked to its selected user and one server/profile; it expires and does not certify every assignment. Quotas remain per-server rolling 30-day policies. Agent-default, explicit and disabled quotas are displayed separately; an inherited default is not an unlimited allowance. The displayed assignment count refers to configured records on enabled nodes. Search filters the loaded page; load additional pages explicitly.

**Preview new user** and **Review access change** now query the shared local planner for create/enable/disable intent. The plan lists every enabled server, current/proposed policy, missing/duplicate records and identity conflicts. It preserves quota blocks and expiry when changing the enabled flag. **Recheck snapshot** rejects a changed local snapshot or request; **Rebuild plan** requires reviewing the resulting scope again. Plans are limited to 100 targets and are not stored execution authorizations. No remote preflight, backup, write or publication is performed. CLI access changes remain separate operations until the journal/locking milestone is implemented.

The same review is available without the desktop:

```bash
ovpn user plan --action create --username example-user --quota-bytes 107374182400
ovpn user plan --action disable --username example-user
# Repeat the same intent with its full returned digest to reject changed local state:
ovpn user plan --action disable --username example-user --expect-digest <digest>
```

**Monitoring** configures Native and Checker schedules and shows history and incidents. **Integrations** installs pinned tools and starts finite Knife diagnostics. See [automatic observation](observation.md) for schedules, limits and lifecycle.

## Check traffic

Open **Checks**, select an enabled server and a TCP/REALITY or TCP/TLS profile, choose an eligible user from the selected server’s list and press **Run HTTPS check**. The request uses the pinned native Xray client and verifies `https://example.com/`, HTTP 200 and its certificate, with a 20-second timeout and a 1 MiB body limit. It does not create users or deploy configuration. Existing encryption keys are resolved as in the CLI; OS-vault migration is not implemented yet.

Runtime health remains **Not checked** when inventory has been read. A successful traffic sample expires after five minutes and does not certify UDP, DNS, IPv6 or GUI-client import. For XHTTP, use the owned Checker or Knife/Xray adapters in Integrations. Automatic observations have a separate lifetime and history; their green results cover only the explicitly selected identities and paths.

## Machine protocol 1

```bash
ovpn machine --stdio --protocol 1
```

Send one JSON object per line. Start every session with:

```json
{"id":"hello-1","method":"engine.hello","params":{"protocolVersion":1}}
{"id":"summary-1","method":"workspace.summary","params":{}}
{"id":"servers-1","method":"servers.list","params":{"limit":50}}
```

Implemented methods include `engine.hello`, `workspace.summary`, `servers.list`, `users.list`, `users.plan`, `checks.users` and `checks.run`. Inventory uses `node-<id>` identifiers and keyset cursors. `users.plan` accepts `action`, `username`, create-only `expiry`/`quotaBytes` and optional `expectedDigest`; it advertises `users.plan.read` without enabling writes. `checks.run` accepts `serverId`, an existing `username` and optional `profile`; it deliberately accepts no custom URL or raw configuration. `checks.users` returns paginated usernames for enabled, unexpired and non-quota-blocked assignments on the selected server. It advertises `checks.users.read`. Additional implemented observation methods are `observer.status/configure/start/stop/remove/run/cancel`, `integrations.install/setup` and `diagnostics.start`. They advertise observation/integration capabilities separately from server/user writes. Proposed server execution methods in the [architecture](design/desktop/architecture.md) remain unavailable.

The worker processes requests sequentially, rejects unknown fields, bounds messages to 1 MiB and returns stable errors without raw database, path or tunnel diagnostics. Its stdout carries protocol envelopes only. The host verifies the worker digest and exact engine/protocol versions, serializes calls and terminates the session on timeout or malformed output. An explicit later action may reconnect; a failed check is not automatically repeated.

## Validation

```bash
go test ./...
make client-compatibility
make test-race
npm --prefix apps/desktop/frontend test
npm --prefix apps/desktop/frontend run build
go -C apps/desktop vet ./...
```

Native macOS launch and the local inventory handshake were exercised during implementation. Windows x64 has a cross-build gate; native three-platform support, screen-reader/scaling coverage and public installer signing remain outstanding. See the [implementation roadmap](design/desktop/roadmap.md) for subsequent stages.
