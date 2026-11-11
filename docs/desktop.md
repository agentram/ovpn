# OVPN Desk

Desk manages the same local workspace as the CLI. It uses Wails, React and a
version-verified Go worker. English is the default; Settings offers Russian,
Simplified Chinese and dark/light themes.

## Start

Build with the repository's Go toolchain, Node.js 26/npm and Python 3.
Native prerequisites follow [Wails installation](https://v3.wails.io/quick-start/installation/).
On macOS, install Xcode Command Line Tools.

```bash
python3 scripts/build_desktop.py
open "bin/desktop/darwin/OVPN Desk.app"
```

The `OVPN_DESK_DATA_DIR` environment variable also selects the startup workspace;
`--data-dir` takes precedence. To use an isolated workspace:

```bash
"bin/desktop/darwin/OVPN Desk.app/Contents/MacOS/ovpn-desk" \
  --data-dir /path/to/workspace
```

The development macOS bundle targets macOS 13+, is ad-hoc signed, and is not
notarized. Windows builds use `--target windows --arch amd64`; Linux builds
require a native Linux host. Keep the matching worker beside the executable.
Native acceptance on all three operating systems remains a release gate.

## Everyday actions

- **Overview:** open a server, user, connection problem or recent operation.
- **Users:** select **Add user**, enter a name and optional quota/end date,
  review every enabled server, then apply. Open a user to change access or
  explicitly reveal a connection link and QR. Search covers the whole loaded
  inventory, including all pages for the supported 100-user workspace.
- **Servers:** select **Add server**, enter its address/domain and SSH key,
  compare the displayed SSH fingerprint with the server console, then confirm
  registration and installation. Existing SSH-agent keys are supported. The
  account needs passwordless sudo. Use a supported Linux host with a domain
  pointing to it. Server details expose update, restart, logs and backups;
  less common profile/backend actions are under **Advanced**.
- **Activity:** inspect operation progress and per-server results, check history
  and diagnostic jobs. A partial change retains completed steps. Resume
  reconciles interrupted user changes; other interrupted writes require a new
  reviewed plan. A changed workspace also requires a fresh review.

Use the matching 1.13 CLI when writing to this workspace; older CLIs do not
participate in the operation journal and writer locks.

User mutations continue to apply to **all enabled servers**. Limits are
per-server rolling 30-day allowances. Changing policy does not reset collected
traffic. Deletion, restore and cleanup require a second confirmation.

A verified backup precedes remote changes. A newly registered server is checked
for an existing runtime before installation; an imported existing installation
is backed up too. Do not equate a completed operation with demonstrated client
connectivity: inspect its connection checks afterwards.

## Automatic checks and live data

Enable automatic checks once after reading the traffic notice. Desk selects
eligible users and enabled profiles itself. It checks access every 15 minutes
and refreshes changed assignments immediately, with four shared traffic slots
and one per server. Native Xray handles TCP/TLS and TCP/REALITY; compatible
XHTTP uses checksum-pinned Checker. Tool preparation starts when needed.
Knife is an explicit advanced diagnostic and requires traffic consent.

Checks run **only while Desk is open**. Service/traffic reads run every
30 seconds and presence reads every 10 seconds. After sleep or source loss,
old measurements stop being current. A connection check confirms access from
this computer; it does not establish availability from every client network.

**Access works**, **Connected now**, **Traffic used** and **Remaining** are
separate facts. Presence comes from Xray online statistics, not a device count.
Older agents need updating to expose presence. During probe activity and its
closing window, presence displays **Updating data** rather than counting the
probe as a person. Probe traffic still consumes a small part of the allowance.
An unavailable collector is unknown, not zero usage.

## Share a setup code

**Create setup code** needs an existing trusted HTTPS subscription service
serving this worker's prepared bundle and invitation directories. Defaults are
`delivery/bundles` and `delivery/invites` within the workspace. Use
`subscription serve` with these directories behind your configured HTTPS
endpoint, or supply existing directories under **Advanced**. The form checks
HTTPS and the exact published snapshot before issuing a 30-minute one-time
code. A failed check revokes the newly prepared delivery bundle.

Revoking profile delivery stops future downloads; disabling VPN access is a
separate user action. Current user traffic never enters diagnostic reports.
Links/codes appear only on explicit delivery actions and are not journaled.

## Architecture and validation

Machine protocol 1 adds `operations.catalog/plan/start/list/result/resume/cancel`
and `desk.snapshot/configure/routes/pause`. Inputs are typed flag values and
positional arguments; the worker uses the same registered management handlers
as the CLI without parsing terminal output. The common coordinator owns plans,
private journals, locking, checkpoints and reconciliation. Existing command
handlers remain composition adapters while domain extraction continues.

See [command coverage](design/desktop/cli-coverage.md),
[testing](testing.md), and [native/pilot acceptance](design/desktop/acceptance.md).
