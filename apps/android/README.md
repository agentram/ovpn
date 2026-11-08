# O Tunnel

O Tunnel is the Android client for an existing OVPN installation. On first launch, choose **Enter code** or **Scan QR code**. You can scan with the camera or open a saved QR image. Select **Connect** and approve the Android VPN prompt. The app supports English, Russian and Simplified Chinese in **Settings**.

Administrators can issue a [one-time setup code](../../docs/subscriptions.md#activate-o-tunnel) for a published user subscription. The app also accepts existing VLESS links and HTTPS subscriptions. Use **Settings → Manage access** to add or refresh profiles.

Choose **Change profile** on the home screen to try another TCP/TLS or TCP/REALITY profile. Open **Problem report** to review and optionally share the latest connection results. Reports are prepared on the phone without profile names, addresses, URLs or keys.

## Build and install

Install JDK 17, the Go version in the repository's `go.mod`, and Android SDK packages `platforms;android-36`, `build-tools;36.0.0`, `platform-tools` and `ndk;28.2.13676358`. Set `JAVA_HOME` and `ANDROID_HOME`.

```sh
cd apps/android
./build-core.sh
./gradlew :app:assembleDebug :app:testDebugUnitTest :app:lintDebug :app:bundleRelease
./check-alignment.sh app/build/outputs/apk/debug/app-debug.apk
"$ANDROID_HOME/platform-tools/adb" install -r app/build/outputs/apk/debug/app-debug.apk
```

The app requires Android 10 or later. The build targets API 36 and includes ARM64 and x86-64. The Go bridge uses the Xray revision pinned in the root module. App version, embedded Xray version and privacy information are in **Settings**.

## Tunnel behavior

Android routes IPv4 and IPv6 into the service's TUN. The only native outbound sockets outside that TUN are protected TCP connections to the resolved VPN endpoint on the selected physical network. Android resolves the endpoint hostname on that network. The VPN uses `1.1.1.1` for user DNS through the tunnel, and checks HTTPS against `www.gstatic.com/generate_204` on the VPN network.

The service keeps the TUN in place while reconnecting after a network change or switching profiles. Disconnect or revoke VPN permission to release the TUN. A failed HTTPS check can indicate an unreachable check endpoint, tunnel failure or revoked server access. The app reports an unverified tunnel when Xray is running and the HTTPS check fails.

See [release preparation](../../docs/android-release.md) and [privacy policy](../../docs/android-privacy.html).
