# Transport Profiles

`ovpn` keeps the original TCP/REALITY profile for compatibility:

```text
vless-reality-tcp-vision = VLESS / TCP / REALITY / xtls-rprx-vision on 443/tcp
```

That profile is widely supported by current clients and existing links keep working. It is still one traffic shape, though, and in degraded networks it has not been the most reliable option.

Transport profiles make that explicit: keep the default compatibility profile live, enable one fallback profile, test it with a small set of users, and switch the server primary profile only if the tradeoff is acceptable. In current field tests, self-SNI TLS and plain XHTTP have been the most reliable fallback shapes on some degraded paths. Plain XHTTP is not transport-encrypted by itself.

## Profiles

| Profile | Status | Port | Use case |
| --- | --- | ---: | --- |
| `vless-reality-tcp-vision` | default | `443/tcp` | Original VLESS + REALITY profile. Best compatibility baseline and kept for existing links. |
| `vless-xhttp-plain` | fallback | `13179/tcp` | Plain XHTTP shape for affected networks; no transport security unless fronted by TLS separately. |
| `vless-tcp-tls-selfsni-web` | camouflage | `443/tcp` | TCP/TLS profile with a real certificate and fallback to an internal static web service. Conflicts with `vless-reality-tcp-vision` because both own `443/tcp`. |
| `vless-xhttp-vlessenc` | experimental | `13180/tcp` | XHTTP with VLESS Encryption. It adds PFS, replay protection, and a ticket-based client handshake, but is not a standalone DPI defence. |

The former `vless-reality-xhttp` and `vless-ws-tls-web` profiles are removed from the supported profile set. When an old local database contains one of those names, `ovpn` drops it while normalizing the server record; it is not listed, rendered, or available for new links. No manual cleanup command is required.

The fallback profiles are not magic. They give you controlled A/B testing and faster rotation when a network path degrades.
The `vless-xhttp-plain` profile deliberately has `security=none`, `path=/`, and no REALITY parameters because it matches simple XHTTP profiles seen working in the field. That means Xray does not add REALITY/TLS transport security on this profile. HTTPS destinations inside the tunnel are still protected by HTTPS, but the VLESS/XHTTP transport itself is not protected like the REALITY profiles. Use it only when that tradeoff is acceptable, and keep monitoring whether it remains reliable for your users.

The `vless-tcp-tls-selfsni-web` profile is different from both REALITY and plain XHTTP. It uses a real certificate for the same domain that clients connect to. Xray owns public `443/tcp`, valid VLESS clients pass through the TLS inbound, and ordinary or invalid HTTPS traffic falls back to an internal `ovpn-web` static site. This improves probing behavior because `https://<domain>/` returns a normal page, but it is not a guarantee that the traffic shape cannot be classified.

`vless-xhttp-vlessenc` uses XHTTP `mode=auto` and Xray's `mlkem768x25519plus.native` VLESS Encryption mode. The server-side decryption value and client-side encryption value are different secrets. ovpn generates them once per VPN cluster, stores them encrypted in local state, and includes only the client value in links and QR codes. Generation runs over SSH on the selected server with its pinned Xray Docker image, so the operator workstation does not need Docker. Do not copy a server-side decryption value into a client or support request.

## Enable and Test

List profiles:

```bash
./ovpn server profile list <server>
```

Enable an extra profile:

```bash
./ovpn server profile enable <server> vless-xhttp-plain
./ovpn deploy <server>
./ovpn doctor <server>
```

Enable experimental VLESS Encryption + XHTTP only after opening its port through the host baseline:

```yaml
# ansible/inventories/production/host_vars/<server-hostname>.yml
ovpn_firewall_extra_tcp_ports:
  - 13180
```

```bash
cd ansible
ANSIBLE_CONFIG=ansible.cfg ansible-playbook -i inventories/production/hosts.yml playbooks/host-maintenance.yml --limit <server-hostname>
cd ..

./ovpn server profile enable <server> vless-xhttp-vlessenc
./ovpn deploy <server>
./ovpn doctor <server>
```

This profile is currently confirmed only with Mihomo. Treat Streisand, Happ, and Hiddify as unconfirmed until they import the link and pass real traffic tests with the installed client version.

Self-SNI owns `443/tcp`, so switch to it instead of enabling it next to TCP/REALITY. The Ansible step only prepares the certificate and fallback site; the profile change happens in local ovpn state and is applied by deploy:

```yaml
# host_vars/<server-hostname>.yml
ovpn_camouflage_enabled: true
ovpn_camouflage_domain: vpn-a.example.net
ovpn_camouflage_cert_email: ops@example.net
```

```bash
# First prepare certs and the fallback site through Ansible.
cd ansible
ANSIBLE_CONFIG=ansible.cfg ansible-playbook -i inventories/production/hosts.yml playbooks/security.yml --limit <server-hostname>
cd ..

./ovpn server profile switch <server> vless-tcp-tls-selfsni-web
./ovpn deploy <server>
./ovpn doctor <server>

# Invalid or ordinary HTTPS traffic should see a normal fallback page.
curl -vk https://<domain>/
```

Generate a profile-specific link:

```bash
./ovpn user link --server <server> --username alice --profile vless-reality-tcp-vision
./ovpn user qr --server <server> --username alice --profile vless-xhttp-plain --out ~/Downloads/alice-plain-xhttp.png
./ovpn user link --server <server> --username alice --profile vless-tcp-tls-selfsni-web
./ovpn user qr --server <server> --username alice --profile vless-xhttp-vlessenc --out ~/Downloads/alice-vlessenc.png
```

REALITY and TLS links default to `fp=firefox`. Existing profiles already imported by users do not change.
Operators can still generate explicit variants:

```bash
./ovpn user link --server <server> --username alice --profile vless-reality-tcp-vision --fingerprint chrome
./ovpn user qr --server <server> --username alice --profile vless-reality-tcp-vision --fingerprint qq --spider-x /assets/alice.js --out ~/Downloads/alice-qq.png
./ovpn user export --server <server> --username alice --profile vless-reality-tcp-vision --fingerprints firefox,qq,chrome --out ~/Downloads
```

`--spider-x` is only for REALITY profiles. If omitted, ovpn generates a stable per-user path, so re-running link export does not create a different client profile each time.
If you want the client spider to start from the target site's root or from a known real path, pass it explicitly, for example `--spider-x /`.
For compatibility checks with older imported REALITY profiles, use `--legacy-reality`; it generates the previous client shape with `fp=chrome` and no `spx`:

```bash
./ovpn user qr --server <server> --username alice --profile vless-reality-tcp-vision --legacy-reality --out ~/Downloads/alice-reality-legacy.png
```

Profile-specific client fields:

| profile | `--fingerprint` | `--spider-x` | SNI source |
| --- | --- | --- | --- |
| `vless-reality-tcp-vision` | yes | yes | `reality_server_name` |
| `vless-tcp-tls-selfsni-web` | yes | no | server domain |
| `vless-xhttp-plain` | no | no | none |
| `vless-xhttp-vlessenc` | no | no | none |

Passing an unsupported field fails before ovpn prints a link or writes a QR code.
For example, `--spider-x` with `vless-tcp-tls-selfsni-web` fails because self-SNI uses normal TLS and fallback routing, not REALITY spidering.

Export all enabled profiles for a user:

```bash
./ovpn user export --server <server> --username alice --all-profiles --out ~/Downloads
```

Optionally switch the default profile used by `user link` when no `--profile` is passed:

```bash
./ovpn server profile switch <server> vless-xhttp-plain
./ovpn deploy <server>
```

Do this only after testing with real clients. Switching the primary profile does not change already issued links; it only changes future link generation when `--profile` is omitted.

Keep the old profile enabled during testing.
After users have migrated, disable a non-primary profile and redeploy:

```bash
./ovpn server profile disable <server> vless-reality-tcp-vision
./ovpn deploy <server>
./ovpn doctor <server>
```

`disable` updates local desired state only. Old links for that profile keep working until the next successful deploy removes the Xray inbound.
The CLI refuses to disable the current primary profile; switch primary first, deploy, verify, then disable the old profile.

Link and QR commands fail early when a requested profile is unknown or disabled. That is intentional: they should not print a secret that cannot work on the selected server. The removed profile names are reported as unsupported.

## REALITY Target And SNI

`reality_target` and `reality_server_name` are server-side settings stored per server. Do not rotate them casually:

- `reality_server_name` must be covered by the TLS certificate presented by `reality_target`.
- The target should be stable, reachable from the VPN host, and behave like a normal high-traffic HTTPS service.
- Region-specific or “domestic” targets can make one network better and another worse; test them deliberately.
- Changing target/SNI requires updating server config, running `ovpn deploy`, and issuing fresh REALITY links.

They do not have to be identical across a multi-server setup. The REALITY key
and short ID remain cluster identity parameters, while target/SNI can be changed
on one test server without blocking its deployment:

```bash
ovpn server set-reality-target <server> www.trip.com
ovpn deploy <server>
```

The command changes local desired state until `deploy` is run. Generate new
links after deployment; existing links keep their previous target/SNI values.

Client-side `--fingerprint` and `--spider-x` variants are safer levers because they change only newly generated links.

## Xray Version And REALITY Failures

An Xray image upgrade and a REALITY target change are different operations.
Test a new image on an unused server first. Check `doctor`, the Xray startup
log, and at least one client from each network that matters before changing
the repository default. Keep the previous image tag available for rollback.

The server and client versions also matter. A newer Xray release can warn or
enforce a minimum client version, so an image upgrade must be checked against
the client applications already used by people. An image-only upgrade should
not require new links, but changing the target, SNI, keys, short ID, flow, or
transport does.

Use the target check from the server when investigating a REALITY failure:

```bash
xray tls ping <reality-target>:443
```

The target must be reachable from the VPN host and its certificate must cover
the configured SNI. This check validates the server-side target; it does not
prove that an ISP will pass the client handshake. Community reports describe
middleboxes dropping fragmented or unusually large ClientHello packets, and
regional filtering can produce the same symptom as a broken server. Treat
those reports as hypotheses and compare server logs, client diagnostics, and a
known-good network.

`spiderX` is only the initial path used after a REALITY handshake. It cannot
repair a handshake that fails before authentication, and changing it alone is
not a reliable way to address operator filtering.

The current repository default is intentionally kept on the stable upstream
Xray pin. Prerelease images can be tested per server with
`server set-xray-version`, but should not be rolled out globally without a
compatibility review. See `docs/upgrades.md` for the backup, deploy,
validation, and rollback sequence.

## Client Compatibility

Client support differs by transport and version:

- TCP + REALITY + vision is the compatibility baseline.
- XHTTP is newer. Test the exact client and version before sending it broadly.
- Plain XHTTP may import in clients that fail REALITY/XHTTP, but it does not provide the same stream-security layer unless you put it behind a separate TLS front.
- VLESS Encryption + XHTTP requires a client implementation that supports the generated `encryption` parameter. Test it first with Mihomo; other clients are not yet confirmed.
- TCP/TLS self-SNI needs a real certificate and a client that supports VLESS TCP TLS with vision flow. It is useful when you want the domain to answer like an ordinary HTTPS endpoint.
- WS/TLS with a real web front is a separate implementation step, not just another link parameter.

When a user reports “connected, but nothing loads”, compare profiles instead of guessing:

```bash
./ovpn user diagnose --server <server> --username alice --since 24h
./ovpn user debug start --server <server> --username alice --duration 15m
./ovpn user debug show --server <server> --username alice --since 15m
```

Look for whether the server sees accepted connections, rejected connections, destination attempts, and source-network changes. If accepted counts rise but traffic stalls, the problem is likely after the profile reaches Xray.

## Operational Notes

- Do not promise that any profile is unblockable. Treat profiles as resilience tools.
- Avoid opening ports you do not use; `ovpn` exposes extra Xray ports only for enabled deployable profiles.
- Do not enable VLESS Encryption on the self-SNI profile: Xray's VLESS `decryption` setting is incompatible with the fallback model used by that profile.
- Use separate users for separate people/devices when you need meaningful diagnostics.
- REALITY failed-auth traffic is handled by its configured target. A local fake website is only technically correct for a separate TLS/WebSocket web-front design.
- The self-SNI profile uses Xray VLESS TCP/TLS fallback. Per Xray's fallback model, fallback is available for VLESS/Trojan TCP+TLS and forwards failed auth or non-VLESS traffic to the configured destination. In `ovpn`, that destination is the internal `ovpn-web` sidecar.

References:

- Xray REALITY transport: https://xtls.github.io/en/config/transports/reality.html
- Xray fallbacks overview: https://xtls.github.io/en/document/level-1/fallbacks-lv1.html
- Xray releases: https://github.com/XTLS/Xray-core/releases
