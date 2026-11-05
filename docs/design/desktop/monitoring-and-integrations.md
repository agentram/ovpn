# Monitoring and integration design

Status: proposed architecture. The current native app provides manual native checks and a clearly labelled monitoring simulation. Neither live automatic probes nor third-party tool execution is enabled by the preview controls.

## What to automate

| Check class | Initial proposed cadence | Identity/scope | Output |
| --- | --- | --- | --- |
| Local policy evaluation | 30–60 s while the workspace is open | All local user records; no tunnel traffic | Enabled/expired/blocked/conflict and missing configured assignment. |
| Agent/runtime observation | 60 s with jitter and backoff | One bounded request per node/source; aggregate agent routes | Agent/Xray state, quota/stat freshness, revision and capability. SSH failure is management evidence. |
| Node/profile HTTPS | 60–300 s, explicitly configured | Dedicated scoped monitoring identity | Actual tunnel result from a named observation source. |
| Customer-identity check | After a reviewed access change or explicit opt-in schedule | Selected identity + node/profile; never silently every user each minute | Evidence of that identity's access, with expiry and charged traffic. |
| Subscription delivery | 5–15 min and after publication | Read-only delivery URL, isolated from control credentials | TLS/status/format/snapshot generation; response credentials discarded after validation. |
| Capacity/certificate/backup | 5–15 min or daily as appropriate | Host/runtime metadata; archive integrity separate from restore | Warning thresholds and freshness. New runtime collectors may be required. |
| Performance/core comparison | Manual initially | Explicit finite selection and byte/time budget | Engine/version/method/units and measured lower bounds. |

These are proposed defaults, not an enabled policy or measured capacity. Reuse existing agent health, total/daily stats, quota and diagnostic capabilities through typed allowlists; additional host metrics need their own adapter. Debug/session data is sensitive and is not a default dashboard payload.

## Observation and state model

Each sample carries workspace/target/assignment identity, credential generation reference, engine/version, observation source, check kind, target policy ID, desired/applied revision when known, observed/received timestamps, duration units, result code, verified scope, remaining unverified scope and expiry. Raw links, UUIDs, private endpoint details and response bodies do not enter ordinary UI/event data.

Use four dimensions: `runtime`, `policy/distribution`, `traffic`, `activity`. A derived overview may summarize them, but retains the underlying explanation. A failed request is red for that measurement immediately; opening a fleet incident requires the configured evidence threshold. Proposed default: suspect on first failure, confirmed after three consecutive failures, recovered after two fresh successes. A single-source failure remains attributed to that source. An unavailable checker/core creates an observer incident, not a node-down claim.

Stale/unknown samples never retain a healthy green indicator. Use receipt-time monotonic freshness with restart/sleep/clock uncertainty; discard/requalify cached samples on resume. Last-known results can remain visible with an explicit stale label. Different target URLs and timing methods cannot be merged into one latency average.

For 24/7 monitoring, run Checker/an observer on an always-on host. Checker REST supplies current observations, not a complete historical database. Preserve history remotely using Prometheus or an adjacent collector/spool; reconnecting a desktop cannot reconstruct missed samples from the latest REST response. Pull redacted history over an authenticated private connection/SSH tunnel. The observer holds prepared monitoring profiles only, without control state or SSH management keys.

## Scheduler and cost model

One owner schedules each `(source, target, profile, kind)` job. Local desktop and external Checker must not both schedule the same identity unintentionally. Persist policy ownership, avoid overlapping cycles and cap global/per-node concurrency. Queries remain responsive through a separate observation execution path; today's sequential machine worker is not a background scheduler.

- Add jitter, deadlines, bounded queues, retry/backoff and per-target circuit state. Do not turn an application wake-up into a fleet-wide burst.
- Initial ceilings: 4 concurrent jobs overall, 1 per node, no automatic speed tests. Re-estimate with real workloads; these are design defaults.
- Show request-count estimate before enabling a policy. For `sources × profiles × identities × targets × 86400 / interval`, 10 nodes with two profiles, one source, one identity and a 60 s interval generate 28,800 probes/day; checking 100 customer identities each would multiply that by 100.
- A 20 MiB per-target/day ceiling with 1,440 probes/day leaves only about 14.6 KiB per probe. The current 1 MiB response cap alone is not a budget guarantee. Use a small bounded response (for example 4 KiB), include an estimate of handshake/transport overhead, track charged runtime traffic where available, and stop before the ceiling. Exact wire-byte accounting needs explicit transport instrumentation.
- Maintain per-job, per-target and daily budgets, including third-party behavior. Provider timeout or max-body limits do not necessarily bound bytes after decoding/transport buffering. Pause and explain a budget exhaustion instead of reporting the path failed.
- Per-user full checks are opt-in, change-triggered or sampled, with a coverage percentage. Node checks use visible service identities with expiry/quota/cleanup; never create them through today's global user-add path unnoticed.

UI configuration: source → target/profile scope → identity policy → cadence → targets → estimated load/budget → lifecycle (while open / background / always-on) → review and enable. Background OS registration is a distinct native capability. Default control operations do not become unattended remediation from a health policy.

## Adapter model

Start with three first-party integrations through documented API/CLI contracts. The word “integration” means an OVPN adapter; it does not assume a shared upstream plugin SDK. A future extension SDK would need its own versioned protocol and enforced permissions.

| Adapter | Role | Input | Allowed output |
| --- | --- | --- | --- |
| Native | Low-overhead explicit and later scheduled HTTPS | Opaque reference to a prepared client profile | Existing credential-free probe result plus provenance/freshness. |
| Xray Checker | Periodic proxy monitoring and metrics | Dedicated prepared subscription/config set | Stable proxy ID, online, measured latency/time, source and supported metrics. |
| Xray Knife | Diagnostic jobs and comparisons | Private finite batch of prepared links | Job/result IDs, engine, status, code, timings, endpoint counts and opt-in bounded performance numbers. |

Version/capability-check each adapter before enabling it. Track owned versus externally managed processes; only stop processes started by OVPN. Pin artifacts and verify their digests. Enforce executable paths, private work directories, environment/argument allowlists, deadlines, cancellation and output limits. A manifest alone is not an OS sandbox. Do not load arbitrary executable plugins into the privileged frontend or embed a third-party web interface there. Imported configurations require structural validation: reject arbitrary file references, executable providers and unexpected listeners, bound batch size, and constrain destinations and protocol capabilities before running them. A private work directory alone does not prevent a tool from reading other files or opening unrestricted network connections.

### Checker adapter

Reference: [configuration source](https://github.com/kutovoys/xray-checker/blob/f829c2b7d44670f4db0f7355f445771e7cde7158/config/config.go), [OpenAPI](https://github.com/kutovoys/xray-checker/blob/f829c2b7d44670f4db0f7355f445771e7cde7158/web/openapi.yaml).

- Validate `/api/v1/status` and `/api/v1/proxies` response envelopes and stable IDs; project only an allowlist. Authenticated endpoints can carry private server/configuration information. Treat optional fields as version-specific.
- The reviewed defaults include all-interface metrics binding, disabled metrics protection, unbounded check concurrency and simulated latency. An owned instance must explicitly bind loopback, enable fresh authentication, set finite concurrency and disable latency simulation. Choose a small HTTPS status target rather than inheriting an HTTP or large-download default.
- Persist a mapping from prepared profile identity to Checker stable ID. Labels alone are insufficient identity. Detect profile/credential changes and stale mappings; do not join by array position.
- Poll at a cadence consistent with Checker sampling; do not relabel repeated old samples as fresh measurements. Clock/skew uncertainty remains visible.
- Connection setup distinguishes local owned process, existing remote instance and always-on history provider. Show permission scope and data placement before preparing profiles.
- Review the project's AGPL terms and distribution obligations before shipping its executable. External process separation does not determine licensing obligations by itself.

### Knife adapter

Reference: [CLI flags](https://github.com/lilendian0x00/xray-knife/blob/a579d42d2ca2b40293c20db0353d39ad4495066b/cmd/http/http.go), [root/database option](https://github.com/lilendian0x00/xray-knife/blob/a579d42d2ca2b40293c20db0353d39ad4495066b/cmd/root.go), [result schema](https://github.com/lilendian0x00/xray-knife/blob/a579d42d2ca2b40293c20db0353d39ad4495066b/pkg/http/examiner.go). This is reviewed main-branch source, not a certified release contract.

A candidate diagnostic invocation uses arguments supplied as an array, with a tested/pinned binary:

```text
xray-knife --db <private-job-db> http
  --file <private-input-file> --out <private-result-file> --type jsonl
  --core xray --threads 2 --timeout 10000 --retries 0
  --url <approved-https-target> --rip=false --diagnose=false
```

This is an adapter design example, not an implemented OVPN command. Probe binary help/schema compatibility before using these flags. Apply the workspace budget and verify provider cancellation/byte behavior in an isolated lab.

- `json`/`jsonl` include all results in the reviewed source. The `link` field contains credentials; `reason`, protocol address and IP metadata can also be sensitive. Parse privately and discard everything outside the allowed observation DTO. Do not pass the original record or command output into UI/history/support bundles.
- `--diagnose` and IP lookup are enabled upstream by default in the reviewed source. Disable both for baseline jobs; direct endpoint diagnostics and extra discovery requests need an explicit separate action. Preserve strict TLS verification.
- Use an isolated job database/work directory, never the operator's existing Knife library by default. Pass profiles through private files/stdin, not command-line link arguments. Suppress raw stdout/stderr persistence and enforce result-file limits.
- Speed tests are explicit, finite experiments with separate bytes/time settings and no periodic default. Scanner/CIDR discovery, rotating local proxy, per-process proxy and TUN mode belong to later privileged-client/lab scopes, not ordinary observation.
- The source is MIT licensed; retain required notices if distributing it. Prefer a separate pinned process adapter over sharing its database or authentication system.

## Incidents, history and retention

Store normalized samples, not full provider output. A proposed starting retention is 7 days of raw observations, 30 days of hourly rollups and 90 days of incident/change summaries, subject to a configurable disk cap. Roll up with counts/gaps and documented percentiles; missing measurements are not zero latency or success. Show truncated history explicitly.

Incident keys include source, target, profile and failure class; relate a shared observer outage to affected views without generating one outage notification per user. Notifications are opt-in and deduplicated; no external uploads/messages occur merely by enabling a local preview. External adapters and metrics should not use customer names as high-cardinality public labels.

## Acceptance gates

- Stop Checker and verify “observer unavailable,” while node state retains its independently observed meaning.
- Change a credential/profile and verify stale mapping/results cannot keep it green.
- Sleep/quit the desktop, resume and verify missed history remains a visible gap unless actually retained remotely.
- Fail one resource while another passes and verify target-specific outcomes.
- Exhaust a traffic budget; pause jobs without calling the node offline.
- Run a batch containing invalid/private metadata; ensure no links, UUIDs, IP metadata or raw errors reach frontend/events.
- Compare the same selected profile/target with Native, Checker and Knife; preserve method/units/core versions rather than presenting incomparable numbers as universal RTT.
