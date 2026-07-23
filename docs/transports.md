# Transport Profiles

`ovpn` keeps the original TCP/REALITY profile for compatibility:

```text
vless-reality-tcp-vision = VLESS / TCP / REALITY / xtls-rprx-vision on 443/tcp
```

That profile is widely supported by current clients and existing links keep working. It is still one traffic shape, though, and in degraded networks it has not been the most reliable option.

Transport profiles make that explicit: keep the deprecated compatibility profile live, enable one fallback profile, test it with a small set of users, and switch the server primary profile only if the tradeoff is acceptable. In current field tests, plain XHTTP has been the most reliable fallback shape on some degraded paths, but it is not transport-encrypted by itself.

## Profiles

| Profile | Status | Port | Use case |
| --- | --- | ---: | --- |
| `vless-reality-tcp-vision` | deprecated | `443/tcp` | Original VLESS + REALITY profile. Best compatibility, less reliable on affected paths. |
| `vless-reality-xhttp` | experimental | `8443/tcp` | XHTTP + REALITY for clients that support XHTTP. Useful as a different traffic shape. |
| `vless-xhttp-plain` | fallback | `13179/tcp` | Plain XHTTP shape for affected networks; no transport security unless fronted by TLS separately. |
| `vless-tcp-tls-selfsni-web` | camouflage | `443/tcp` | TCP/TLS profile with a real certificate and fallback to an internal static web service. Conflicts with `vless-reality-tcp-vision` because both own `443/tcp`. |
| `vless-ws-tls-web` | planned | n/a | WebSocket/TLS behind a real HTTPS site. Not deployable yet because it needs certificate and web-front handling. |

The fallback profiles are not magic. They give you controlled A/B testing and faster rotation when a network path degrades.
The `vless-xhttp-plain` profile deliberately has `security=none`, `path=/`, and no REALITY parameters because it matches simple XHTTP profiles seen working in the field. That means Xray does not add REALITY/TLS transport security on this profile. HTTPS destinations inside the tunnel are still protected by HTTPS, but the VLESS/XHTTP transport itself is not protected like the REALITY profiles. Use it only when that tradeoff is acceptable, and keep monitoring whether it remains reliable for your users.

The `vless-tcp-tls-selfsni-web` profile is different from both REALITY and plain XHTTP. It uses a real certificate for the same domain that clients connect to. Xray owns public `443/tcp`, valid VLESS clients pass through the TLS inbound, and ordinary or invalid HTTPS traffic falls back to an internal `ovpn-web` static site. This improves probing behavior because `https://<domain>/` returns a normal page, but it is not a guarantee that the traffic shape cannot be classified.

## Enable and Test

List profiles:

```bash
./ovpn server profile list <server>
```

Enable an extra profile:

```bash
./ovpn server profile enable <server> vless-reality-xhttp
./ovpn server profile enable <server> vless-xhttp-plain
./ovpn deploy <server>
./ovpn doctor <server>
```

Self-SNI owns `443/tcp`, so switch to it instead of enabling it next to TCP/REALITY. The Ansible step only prepares the certificate and fallback site; the profile change happens in local ovpn state and is applied by deploy:

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
./ovpn user link --server <server> --username alice --profile vless-reality-xhttp
./ovpn user qr --server <server> --username alice --profile vless-reality-xhttp --out ~/Downloads/alice-xhttp.png
./ovpn user qr --server <server> --username alice --profile vless-xhttp-plain --out ~/Downloads/alice-plain-xhttp.png
./ovpn user link --server <server> --username alice --profile vless-tcp-tls-selfsni-web
```

REALITY and TLS links default to `fp=firefox`. Existing profiles already imported by users do not change.
Operators can still generate explicit variants:

```bash
./ovpn user link --server <server> --username alice --profile vless-reality-xhttp --fingerprint chrome
./ovpn user qr --server <server> --username alice --profile vless-reality-xhttp --fingerprint qq --spider-x /assets/alice.js --out ~/Downloads/alice-qq.png
./ovpn user export --server <server> --username alice --profile vless-reality-xhttp --fingerprints firefox,qq,chrome --out ~/Downloads
```

`--spider-x` is only for REALITY profiles. If omitted, ovpn generates a stable per-user path, so re-running link export does not create a different client profile each time.

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

Link and QR commands fail early when a requested profile is unknown, planned, or disabled. That is intentional: they should not print a secret that cannot work on the selected server.

## REALITY Target And SNI

`reality_target` and `reality_server_name` are server-side settings. Do not rotate them casually:

- `reality_server_name` must be covered by the TLS certificate presented by `reality_target`.
- The target should be stable, reachable from the VPN host, and behave like a normal high-traffic HTTPS service.
- Region-specific or “domestic” targets can make one network better and another worse; test them deliberately.
- Changing target/SNI requires updating server config, running `ovpn deploy`, and issuing fresh REALITY links.

Client-side `--fingerprint` and `--spider-x` variants are safer levers because they change only newly generated links.

## Client Compatibility

Client support differs by transport and version:

- TCP + REALITY + vision is the compatibility baseline.
- XHTTP is newer. Test the exact client and version before sending it broadly.
- Plain XHTTP may import in clients that fail REALITY/XHTTP, but it does not provide the same stream-security layer unless you put it behind a separate TLS front.
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
- Use separate users for separate people/devices when you need meaningful diagnostics.
- REALITY failed-auth traffic is handled by its configured target. A local fake website is only technically correct for a separate TLS/WebSocket web-front design.
- The self-SNI profile uses Xray VLESS TCP/TLS fallback. Per Xray's fallback model, fallback is available for VLESS/Trojan TCP+TLS and forwards failed auth or non-VLESS traffic to the configured destination. In `ovpn`, that destination is the internal `ovpn-web` sidecar.

References:

- Xray REALITY transport: https://xtls.github.io/en/config/transports/reality.html
- Xray fallbacks overview: https://xtls.github.io/en/document/level-1/fallbacks-lv1.html
- Xray releases: https://github.com/XTLS/Xray-core/releases
