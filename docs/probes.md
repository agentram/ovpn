# Traffic probes and Xray Checker

`doctor` checks the server runtime. `user probe` sends an HTTPS request through a user's VLESS configuration from the operator machine, using the Xray core pinned in the CLI build. It needs no local Docker or separate Xray executable.

## One-shot check

```bash
./ovpn user probe --server <server> --username <probe-user>
./ovpn user probe --server <server> --username <probe-user> \
  --profile vless-reality-tcp-vision \
  --url https://example.com/ --expect-status 200 \
  --timeout 20s --max-bytes 1048576 --json
```

Success returns exit code zero. JSON results contain `success`, `status_code`, `duration_ms`, `ttfb_ms`, and `bytes`; a failed request adds `error_code` and returns a nonzero exit code. Input or core initialization errors are reported on stderr before a request result is available. URLs, response bodies, UUIDs and keys are excluded from results.

TLS certificates are verified. Redirects and environment proxy settings are ignored, and there is no direct-connection fallback. The request timeout includes reading the response body. TTFB measures time to the first response byte, including tunnel setup, rather than network RTT.

Built-in probes support TCP/REALITY and TCP/TLS self-SNI. Use an enabled profile and an active, unexpired user. Remote quota enforcement or a stale deployment can still reject the connection. Export XHTTP profiles to the separate checker for testing; see [transport profiles](transports.md) for their security and client compatibility limits.

If `doctor` passes but a newly added user fails the probe, compare the deployed agent with the CLI release and inspect `server logs <server> --service xray --tail 200`. An older agent can add a self-SNI TLS user without the required Vision flow even when the saved configuration is correct.

The result describes the route from the machine running the probe. Run it from the networks you need to observe. A successful check does not establish connectivity from every client network. Each request consumes traffic for the selected user.

## Continuous checks

Use a dedicated monitoring user. Creating or removing users applies to all enabled servers; export and probe commands are server-scoped.

```bash
mkdir -m 700 checker-configs
./ovpn user export --server <server> --username <probe-user> \
  --format xray-checker --out ./checker-configs
```

Add `--all-profiles` to export every enabled profile, or `--profile <profile>` to select one. Existing link/QR exports retain their default format. JSON files are written atomically with permissions `0600`; they contain client credentials, never server private keys or agent tokens. Regenerate them after changing endpoints, profiles or credentials. Removing a profile from a later export does not delete an older file: keep only configurations that should be monitored in the directory.

Run [Xray Checker](https://github.com/kutovoys/xray-checker) separately, using its [configuration folder input](https://xray-checker.kutovoy.dev/configuration/subscription/):

```bash
docker run --rm --name ovpn-checker \
  -p 127.0.0.1:2112:2112 \
  -v "$PWD/checker-configs:/configs:ro" \
  -e SUBSCRIPTION_URL=folder:///configs \
  -e PROXY_CHECK_METHOD=status \
  -e PROXY_STATUS_CHECK_URL=https://example.com/ \
  kutovoys/xray-checker:v1.3.1
```

Scrape `/metrics` on port `2112` from the monitoring system. Checker's `xray_proxy_status` reports availability and `xray_proxy_latency_ms` reports response timing. Keep its API and metrics private: labels can reveal endpoint metadata. For deployment outside the local machine, configure network access and authentication according to the [checker documentation](https://xray-checker.kutovoy.dev/).

Give checker only the exported directory; it does not need SSH access, the local ovpn database, or agent tokens. The checker core is independent of the CLI core, so validate the chosen checker release with each exported profile before relying on its alerts. User removal or credential rotation revokes the access contained in existing export files.
