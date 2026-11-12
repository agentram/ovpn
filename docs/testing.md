# Testing strategy

This project uses fast unit tests with mocked orchestration boundaries.

## Coverage areas

- Xray config rendering and validation (`internal/xraycfg`)
- model validation (`internal/model`)
- deploy orchestration sequencing (`internal/deploy`)
- SSH command and copy behavior (`internal/ssh`)
- stats and quota logic (`internal/stats`)
- local and remote SQLite stores (`internal/store/local`, `internal/store/remote`)
- CLI behavior and flag validation (`internal/cli`)
- logging redaction and sensitive output filtering (`internal/logx`)

## Out of scope for unit tests

- real SSH sessions
- real remote hosts
- real Docker daemon interactions
- real Xray runtime processes

Use the integration suites below and operator rollout checks for these boundaries.

## Local test commands

```bash
go test ./...
go test -covermode=atomic -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
make test-race
```

Makefile shortcuts:

```bash
make test
make test-cover
make test-race
```

The race target disables `checkptr` only for the pinned Xray VLESS package, whose Vision implementation inspects TLS connections through reflect-based pointer access. Race instrumentation remains enabled, including the native client traffic tests. Built-in probes reject XHTTP; its pinned client implementation has an upstream race in the stream reader. XHTTP client configurations can still be exported for a separate checker process.

## CI expectations

- `ci.yml` runs tests, coverage, lint, and config checks
- machine-readable test output is captured for artifacts
- ansible syntax/lint checks run in CI

## Release journeys before merge

The release coverage combines browser and device journeys with protocol tests.
Each journey creates a temporary workspace and uses synthetic users and credentials.
Never point these tests at an operator workspace, deployed server, or personal device.

| Feature | Regression coverage |
| --- | --- |
| First server, TLS/REALITY links, XHTTP Vision, Checker export | Deployment and CLI tests cover bootstrap sequencing, generated profiles, and private exports. `make client-compatibility` exercises real client cores with accepted and rejected credentials. |
| CLI subscription delivery | A real CLI process exports only eligible profiles, serves the published snapshot, refreshes it, rotates its URL, and rejects old or revoked tokens. Revocation must leave user policy and links intact. |
| CLI probes, scheduling and tools | Start and stop a real observer, reject a second owner, require consent, retain stale history, and enforce the daily budget after restart. Install checksum-pinned Checker and Knife and test real HTTPS with valid and invalid identities. Incident transitions and tool compatibility edges are tested in the Go packages. |
| OVPN Desk | Chromium drives production UI, Go bindings and the verified worker: complete inventory search, reviewed creation scope, explicit QR delivery, positional server actions, three languages, large text and stale-plan rejection. Required native build/host checks run on macOS, Windows and Linux. |
| Shared operations and monitoring | Independent processes test writer exclusion and kill-after-write reconciliation. Queue tests cover 1,000 assignments, four shared traffic slots, per-server exclusion, changed targets, sleep and probe presence masking. SQLite snapshots include committed WAL pages; unavailable traffic/presence remains unknown. |
| O Tunnel | Android emulator drives setup with a one-time code or saved QR, permission denial and grant, REALITY and TLS, profile switching, an invalid identity, local report redaction, language selection, disconnect, and unavailable subscription recovery. A private DNS/HTTPS fixture proves traffic crossed TUN. Kotlin and Go tests cover malformed input, snapshot replacement, and transport compatibility. |

Run from the repository root:

```bash
make e2e-cli
make e2e-tools
make e2e-ha
npm --prefix apps/desktop/frontend ci
npm --prefix apps/desktop/frontend exec -- playwright install chromium
make e2e-desktop
OVPN_E2E_ANDROID_SERIAL=emulator-5554 make e2e-android
```

`e2e-tools` downloads the pinned binaries and uses a public HTTPS probe
destination. Desk uses Chromium in place of the native WebView transport;
the CI native bundle and Windows cross-build run separately. Android needs
JDK 17, the SDK/NDK from `.github/workflows/e2e.yml`, and a freshly booted,
rootable API 36 emulator. The test installs an ephemeral CA in the emulator,
clears O Tunnel's data, and adds a QR image. Discard the emulator after use.

App-only changes select their matching suite. Shared Go, library, test and
workflow changes run all suites. Documentation-only changes skip the runtime
suites. The required **Go Quality** status fails if a selected suite fails or
does not run. Physical camera, Wi-Fi/mobile handover, accessibility, and live
VPS rollout remain device or operator acceptance checks.

Native builds do not replace the [three-platform and five-person acceptance gates](design/desktop/acceptance.md).

`make e2e-ha` uses the pinned HAProxy image and isolated Docker containers to
check real TCP forwarding, loss of one backend, all-down fail-closed behavior
and recovery. It also exercises the production HA health parser. It does not
replace TLS/REALITY authentication or full SSH deployment/restore acceptance.
The CLI E2E job runs this check before the required Go Quality gate.
