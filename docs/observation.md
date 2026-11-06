# Automatic observation and diagnostic tools

OVPN can schedule native and Xray Checker HTTPS checks and run finite Xray Knife diagnostics. Observation reads existing client identities; it does not create users, deploy servers or change their policies. Select a dedicated existing monitoring identity where possible. Explicitly authorize checks charged to its traffic quota.

## Desktop

1. Select **Local workspace**.
2. In **Integrations**, install the pinned Checker or Knife version if needed. Native checks need no extra tool.
3. In **Monitoring**, choose **New policy**, select Native or Checker, add the exact node/profile/username targets, review the cadence and daily attempt allowance, and enable the policy.
4. Inspect latest samples, history, incidents and jobs. Users and Servers show automatic observations separately from manual checks.

The default cadence is 300 seconds, with up to 10 explicit targets per policy and 10 policies. One workspace owner executes one job at a time; targets within a job run sequentially. Cycles do not overlap or catch up after sleep. Successful completion is followed by the configured interval plus up to 15 seconds of jitter. Enabling a duplicate source/target schedule is rejected.

For Knife, select **Integrations**, choose Xray or sing-box, add targets and run the diagnostic. Native and sing-box selections are limited to the TCP profiles; use Checker or Knife/Xray for XHTTP. A tool's unsupported profile is an observer issue, not proof that a node is offline. Speed tests, IP lookup, direct prescan and extra failure diagnosis are disabled.

Checks run while the application is open. Switching to Demo stops work owned by that window; a separate foreground observer runs independently. Saved policies do not resume simply because the application starts: use **Resume enabled policies**. Demo actions are simulations and never start real tools or checks.

## CLI

Install the optional tools:

```bash
ovpn observe install checker
ovpn observe install knife
```

Save an explicitly enabled policy, then keep its observer running:

```bash
ovpn observe policy add --name baseline --engine native \
  --server <server> --username <monitor-user> \
  --interval 300 --daily-limit 300 --enable --confirm-traffic
ovpn observe run
```

`observe run` is a foreground process; use Ctrl+C to stop it. It uses the existing local workspace. For uninterrupted observation, keep that workspace host and this process running. A separate remote collector with client-only credentials, OS service registration and remote history synchronization remain future work; do not copy management credentials to a new host just to run checks.

Inspect or run a finite check:

```bash
ovpn observe status
ovpn observe once <policy-id> --confirm-traffic
ovpn observe diagnose --engine knife-xray \
  --server <server> --username <monitor-user> --confirm-traffic
ovpn observe policy pause <policy-id>
```

Status and completed jobs use credential-free JSON. A finite check exits unsuccessfully if any result did not pass. Another process can read status/history while an owner is running; stop that owner before changing policies or tools from another process.

For a separately obtained supported binary, `observe tool-set --engine <checker|knife> --binary <absolute-path> --sha256 <binary-digest>` checks its hash and version. Automatic installation pins Checker 1.3.1 and Knife 11.5.0 archives for Linux/macOS/Windows amd64/arm64, extracts only the executable and retains upstream license/source notices. Platform archive availability does not establish native validation on every platform.

## Limits and evidence

- The fixed measurement target is `https://www.gstatic.com/generate_204`, with verified TLS and expected HTTP 204. Native checks cap the response body at 4 KiB. Third-party timing is identified by provider/version; it is not a universal network RTT.
- The daily allowance counts **reserved logical check attempts per source/target and UTC day**. It survives restarts and charges interrupted attempts. It does not claim to measure or cap wire bytes, TLS overhead or runtime traffic. Knife diagnostics reserve at most 20 attempts per target/core/day. Speed tests are not exposed.
- History is separate from the control database, under `observations/`: up to 5,000 samples from the last 7 days. Queries exclude expired samples; the next write removes them from storage. Queries show bounded recent history. Missing intervals are gaps, not successful checks or zero latency.
- Three consecutive traffic failures open a traffic incident; two successful measurements recover it. Tool failure, invalid output, unsupported profiles, inactive local users and exhausted budgets have separate outcomes. They cannot establish a node outage.
- A sample is bound to its selected identity/profile configuration and process session. Credential changes, source loss, restart, sleep, clock uncertainty and expiry prevent old results from remaining green. No sample certifies every user, assignment, runtime process, UDP, DNS, IPv6 or GUI-client import.
- Third-party tools receive generated client-only profiles in private temporary jobs. Raw client links, provider errors, endpoint/IP metadata and response bodies are not stored in observations or sent to the UI. Tool stdout/stderr are discarded; execution time, result size and private job files are bounded. Executables are hash checked before use.
- Tools execute as the current OS user; process separation is not an OS sandbox. The desktop owns their process lifecycle using a Unix process group or a Windows Job Object. Native Windows lifecycle and Linux desktop integration still require platform testing.

Checker runs one cycle with measured latency, finite concurrency and subscription updates disabled. A private tokenized loopback receiver ingests its metric result; its dashboard server is not started. No external IP lookup or geodata download is needed for these generated profiles. Knife returns allowlisted JSONL results bound to the exact prepared link, including all-failed jobs that exit nonzero.

Contracts reviewed: [Checker 1.3.1](https://github.com/kutovoys/xray-checker/tree/v1.3.1) and [Knife 11.5.0](https://github.com/lilendian0x00/xray-knife/tree/v11.5.0). Their upstream licenses apply to the downloaded tools.
