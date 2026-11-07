# OVPN Tunnel release

OVPN Tunnel uses Android's `VpnService` to encrypt device traffic to the user's VPN endpoint. The Play build supports VLESS TCP/TLS and TCP/REALITY with Vision. XHTTP is excluded. See [Google Play's VPN policy](https://support.google.com/googleplay/android-developer/answer/12564964?hl=en).

## Device acceptance

Use a dedicated OVPN deployment with disposable access. Verify HTTPS and DNS, TCP and UDP, and IPv4 and IPv6 from a separate app over both transports. Reject an incorrect REALITY key and an untrusted TLS certificate. Confirm Disconnect and permission revocation remove the TUN. Switch Wi-Fi and mobile data and verify the same profile recovers without direct traffic during the handover.

Import an OVPN QR, a VLESS link and a mixed subscription. Refresh a subscription with a removed or replaced active profile, then try an unavailable URL and a malformed response. Check permission denial, sleep and resume, both languages and themes, large fonts and TalkBack. Record platform version, test result and sanitized failure stage. Use an ARM64 phone and check the 16 KiB build on a compatible device or emulator.

## Upload bundle

Follow the [build instructions](../apps/android/README.md). Supply the upload keystore and its credentials through `OVPN_ANDROID_KEYSTORE`, `OVPN_ANDROID_STORE_PASSWORD`, `OVPN_ANDROID_KEY_ALIAS` and `OVPN_ANDROID_KEY_PASSWORD`. Build with `./gradlew :app:bundleRelease` and verify the resulting AAB with `jarsigner -verify`. Increase Android's `versionCode` for each upload. The ordinary development build produces an unsigned AAB.

Run the [16 KiB checks](https://developer.android.com/guide/practices/page-sizes) on the APK and use `bundletool dump config --bundle=<aab>` to confirm `PAGE_ALIGNMENT_16K`. Check the current [Play target SDK requirement](https://developer.android.com/google/play/requirements/target-sdk) before uploading.

## Play Console

- Publish the [privacy policy](android-privacy.html) and use its public URL in Play Console. Document the use of `VpnService` in the listing.
- Complete the VPN and `systemExempted` foreground service declarations and Data Safety based on the shipped build. Profiles remain on the device. The user's VPN server, subscription provider and the connectivity check and DNS providers are network recipients described in the privacy policy.
- Record a short video showing profile import, the Android consent dialog, a confirmed connection and disconnect. Keep the QR and link out of the recording. Supply a dedicated test profile in the restricted reviewer instructions.
- Promote Internal Testing to Closed Testing and then Production after device checks and review. Qualifying personal accounts need 12 continuously opted-in testers for 14 days. [Play testing requirements](https://support.google.com/googleplay/android-developer/answer/14151465)
