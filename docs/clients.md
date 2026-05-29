# VPN client guide (iOS, Android, Windows, macOS)

This guide describes client setup with a personal `vless://` link for the `ovpn` service.

## Table of contents

- [1. Security: use official clients only](#en-section-1)
- [2. What to ask from the administrator](#en-section-2)
- [3. iPhone (iOS): Streisand](#en-section-3)
- [4. If the app is not available in the App Store: Apple Account region](#en-section-4)
- [5. Android: v2rayNG or Hiddify](#en-section-5)
- [6. Windows: v2rayN or Hiddify](#en-section-6)
- [7. macOS: Hiddify](#en-section-7)
- [8. FAQ](#en-section-8)
- [9. Practical community notes](#en-section-9)
- [10. Official sources](#en-section-10)

<a id="en-section-1"></a>
## 1. Security: use official clients only

App stores and download sites may contain apps with similar names.
Install clients only from official links:

- iOS: Streisand (App Store)
  `https://apps.apple.com/us/app/streisand/id6450534064`
- Android: v2rayNG (GitHub Releases)
  `https://github.com/2dust/v2rayNG/releases`
- Android / iOS / Windows / macOS: Hiddify (official GitHub)
  `https://github.com/hiddify/hiddify-app`
- iOS / macOS: Hiddify (App Store)
  `https://apps.apple.com/us/app/hiddify-proxy-vpn/id6596777532`
- Android / iOS / Windows / macOS: Hiddify (Google Play)
  `https://play.google.com/store/apps/details?id=app.hiddify.com`
- Android / iOS / Windows / macOS: Hiddify (GitHub Releases)
  `https://github.com/hiddify/hiddify-app/releases`
- Windows / Linux / macOS: v2rayN (official GitHub)
  `https://github.com/2dust/v2rayN`
- Windows / Linux / macOS: v2rayN (GitHub Releases)
  `https://github.com/2dust/v2rayN/releases`

Before installing, check:

1. The link opens App Store, Google Play, or the official project GitHub repository.
2. The app name and developer match the official source.
3. You are not using APK/EXE files from third-party download aggregators.

<a id="en-section-2"></a>
## 2. What to ask from the administrator

Ask the administrator for:

- your personal VPN link in `vless://...` format
- a QR code for the same link for quick mobile import

Important:

- treat the link and QR code as a secret credential
- do not post the link in public chats or social networks
- do not forward the link to people who should not use your VPN account

<a id="en-section-3"></a>
## 3. iPhone (iOS): Streisand

### Step 1. Install

1. Open the App Store.
2. Open the official Streisand link:
   `https://apps.apple.com/us/app/streisand/id6450534064`
3. Tap `Get`.

### Step 2. Import the link

1. Copy your VPN link to the clipboard.
2. Open Streisand.
3. Tap `+` to add a profile.
4. Import from clipboard/link, or scan the QR code if you have it.
5. Save the profile.

### Step 3. Connect

1. Open the created profile.
2. Tap `Connect`.
3. Confirm the iOS system request to add a VPN configuration.

### Step 4. Check

1. Open a browser.
2. Visit `https://ifconfig.me` or `https://ipinfo.io`.
3. Check that the IP address changed.

### Step 5. If it does not connect

1. Fully close and reopen Streisand.
2. Delete the profile and import the link again.
3. Check that iPhone date and time are set automatically.
4. Disable and re-enable Wi-Fi or mobile data.
5. Ask the administrator for a fresh link.

<a id="en-section-4"></a>
## 4. If the app is not available in the App Store: Apple Account region

Follow Apple official instructions if you decide to change your Apple Account region.

Before changing the region, check:

1. Apple Account balance is `0`.
2. Active subscriptions are cancelled and fully ended.
3. There are no pending purchases, preorders, rentals, or refunds.
4. If you are in Family Sharing, you may need to leave the family group first.

Path on iPhone:

1. `Settings`.
2. Tap your name.
3. `Media & Purchases`.
4. `View Account`.
5. `Country/Region`.
6. `Change Country or Region`.
7. Select the target country.
8. Accept Terms & Conditions.
9. Fill in payment method and billing/contact details accepted by Apple for that region.

Use valid billing/contact information that Apple accepts for the selected region.
Do not use someone else's address or invented personal details.

Field format:

- Street: `<street and house number>`
- City: `<city>`
- State: `<2-letter state code, for example CA>`
- ZIP: `<5 digits, for example 10001>`
- Phone: `+1` + `<10 digits>`

Examples of the expected field format when payment method `None` (`No Payment Method`) is available:

1. New York, NY
   - Street: `123 Main St`
   - City: `New York`
   - State: `NY`
   - ZIP: `10001`
   - Phone: `+1 212 555 0137`
2. Los Angeles, CA
   - Street: `742 Evergreen Terrace`
   - City: `Los Angeles`
   - State: `CA`
   - ZIP: `90001`
   - Phone: `+1 213 555 0142`

<a id="en-section-5"></a>
## 5. Android: v2rayNG or Hiddify

### Option A: v2rayNG

#### Install

1. Open the official repository:
   `https://github.com/2dust/v2rayNG`
2. Use GitHub Releases for direct downloads:
   `https://github.com/2dust/v2rayNG/releases`
3. Install the current official version.

#### Import and connect

1. Copy your VPN link.
2. Open v2rayNG.
3. Tap `+`.
4. Import from clipboard/link, or scan the QR code.
5. Select the profile and connect.
6. Confirm the Android system VPN request.

### Option B: Hiddify

#### Install

1. Open Google Play or the official GitHub repository:
   `https://github.com/hiddify/hiddify-app`
2. Google Play:
   `https://play.google.com/store/apps/details?id=app.hiddify.com`
3. GitHub Releases:
   `https://github.com/hiddify/hiddify-app/releases`
4. Install the official version.

#### Import and connect

1. Copy your VPN link.
2. Open Hiddify.
3. Import from link/clipboard, or scan the QR code.
4. Save the profile.
5. Tap `Connect`.
6. Confirm the system VPN request.

### Check

1. Open `https://ifconfig.me`.
2. Check that the IP address changed.

### If the app is not available in Google Play

Google says Play country availability depends on your Google Play country.
Changing the country may require:

1. Being physically located in the new country.
2. A payment method from the new country.
3. Waiting between country changes.

<a id="en-section-6"></a>
## 6. Windows: v2rayN or Hiddify

### Option A: v2rayN

#### Step 1. Install

1. Open the official repository:
   `https://github.com/2dust/v2rayN`
2. Open Releases:
   `https://github.com/2dust/v2rayN/releases`
3. Download the archive with the core, usually named like `v2rayN-With-Core...zip`.
4. Extract it to a separate folder.
5. Run `v2rayN.exe`.

#### Step 2. Import the link

1. Copy your VPN link.
2. Import it from clipboard/link in v2rayN.
3. Check that the profile appeared in the list.

#### Step 3. Connect

1. Select the profile.
2. Enable the connection.
3. If needed, enable `System Proxy` or `TUN`.

#### Step 4. Check

1. Open `https://ifconfig.me`.
2. Check that the IP address changed.

#### Step 5. If it does not work

1. Run v2rayN as administrator.
2. Check that the `Xray` core is used.
3. If only some sites do not open, check `TUN` first.
4. Switch `System Proxy` / `TUN` mode and test again.
5. Import the link again.

### Option B: Hiddify (Windows)

#### Step 1. Install

1. Open the official repository:
   `https://github.com/hiddify/hiddify-app`
2. Open Releases:
   `https://github.com/hiddify/hiddify-app/releases`
3. Download and install the Windows version.

#### Step 2. Import and connect

1. Copy your VPN link.
2. Open Hiddify.
3. Import from link/clipboard, or scan the QR code.
4. Save the profile.
5. Tap `Connect`.

#### Step 3. Check

1. Open `https://ipinfo.io` or `https://ifconfig.me`.
2. Check that the IP address changed.

<a id="en-section-7"></a>
## 7. macOS: Hiddify

### Step 1. Install

1. Open the official repository:
   `https://github.com/hiddify/hiddify-app`
2. Open Releases:
   `https://github.com/hiddify/hiddify-app/releases`
3. If you prefer the App Store version, use:
   `https://apps.apple.com/us/app/hiddify-proxy-vpn/id6596777532`
4. Install the app.

### Step 2. Import the link

1. Copy your VPN link.
2. Open Hiddify.
3. Import from clipboard/link.
4. Save the profile.

### Step 3. Connect

1. Select the profile.
2. Tap `Connect`.
3. Confirm macOS system permissions for VPN/network extension.

### Step 4. Check

1. Open `https://ipinfo.io`.
2. Check that the IP address changed.

<a id="en-section-8"></a>
## 8. FAQ

### Why does the link contain `encryption=none`?

This is normal for `VLESS`. Protection is provided by the `REALITY/TLS` transport.

### Do I need to change SNI manually?

Usually no. If the administrator gave you a ready link, the parameters are already set.

### Why does the VPN connect but some sites do not open?

The usual reason is routing mode or the system proxy setting in the client.
For `v2rayN` on Windows, check `TUN` first, then switch `System Proxy` / `TUN` mode and test the IP again.

<a id="en-section-9"></a>
## 9. Practical community notes

Based on common Apple Community and Reddit discussions, users most often hit these issues:

1. App Store region cannot be changed because of active subscriptions or remaining account balance.
2. Payment method is rejected because the card/address country does not match the selected store.
3. Google Play country change is not immediately available and may require waiting.

These are not official project rules, but they match the limitations described in Apple and Google help pages.

<a id="en-section-10"></a>
## 10. Official sources

- Apple: Change your Apple Account country or region
  `https://support.apple.com/en-us/118283`
- Apple: Payment methods that you can use with your Apple Account
  `https://support.apple.com/en-us/111741`
- Google Play Help: How to change your Google Play country
  `https://support.google.com/googleplay/answer/7431675`
- Project X client documentation
  `https://xtls.github.io/document/level-0/ch08-xray-clients.html`

Official client pages:

- Streisand (iOS)
  `https://apps.apple.com/us/app/streisand/id6450534064`
- v2rayNG
  `https://github.com/2dust/v2rayNG`
- v2rayNG (Releases)
  `https://github.com/2dust/v2rayNG/releases`
- v2rayN
  `https://github.com/2dust/v2rayN`
- v2rayN (Releases)
  `https://github.com/2dust/v2rayN/releases`
- Hiddify
  `https://github.com/hiddify/hiddify-app`
- Hiddify (App Store)
  `https://apps.apple.com/us/app/hiddify-proxy-vpn/id6596777532`
- Hiddify (Google Play)
  `https://play.google.com/store/apps/details?id=app.hiddify.com`
- Hiddify (Releases)
  `https://github.com/hiddify/hiddify-app/releases`

Russian version: [`docs/clients-ru.md`](clients-ru.md).
