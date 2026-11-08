"""Drive the real Android app on a disposable, rootable API 36 emulator."""
from pathlib import Path
import os
import re
import shlex
import subprocess
import xml.etree.ElementTree as ET

from run import ROOT, PRIVATE, background, check, command, port, until

PACKAGE = "io.github.agentram.ovpn.tunnel"
ACTIVITY = PACKAGE + "/.MainActivity"


class Emulator:
    def __init__(self):
        self.serial = os.environ.get("OVPN_E2E_ANDROID_SERIAL", "")
        check(re.fullmatch(r"emulator-\d+", self.serial), "Set OVPN_E2E_ANDROID_SERIAL to the disposable emulator")
        self.adb = Path(os.environ["ANDROID_HOME"]) / "platform-tools/adb"
        self.run("wait-for-device", timeout=120)
        until(lambda: self.shell("getprop", "sys.boot_completed").strip() == "1", "Android boot", 150)
        check(self.shell("getprop", "ro.kernel.qemu").strip() == "1", "Physical devices cannot run this suite")
        check(self.shell("getprop", "ro.build.version.sdk").strip() == "36", "Use a rootable Android 36 image")

    def run(self, *args, timeout=40, capture=True):
        try:
            result = subprocess.run((self.adb, "-s", self.serial, *args),
                                    capture_output=capture, text=True, timeout=timeout)
        except subprocess.TimeoutExpired:
            raise AssertionError(f"ADB {args[0]} timed out") from None
        check(result.returncode == 0, f"ADB {args[0]} failed")
        return result.stdout or ""

    def shell(self, *args, timeout=25):
        return self.run("shell", shlex.join(map(str, args)), timeout=timeout)

    def trust(self, root):
        self.run("root")
        self.run("wait-for-device")
        check(self.shell("id", "-u").strip() == "0", "The emulator image must support adb root")
        cert_hash = command("openssl", "x509", "-subject_hash_old", "-in", root,
                            "-noout", text=True, capture_output=True).stdout.strip()
        target = "/data/local/tmp/ovpn-e2e-certs"
        ca_path = "/apex/com.android.conscrypt/cacerts"
        self.shell("mkdir", "-p", target)
        self.shell("sh", "-c", f"cp {ca_path}/* {target}/")
        self.run("push", root, f"{target}/{cert_hash}.0")
        self.shell("sh", "-c", f"chmod 644 {target}/* && chcon u:object_r:system_file:s0 {target} {target}/*")
        zygotes = self.shell("sh", "-c", "pidof zygote zygote64 || true").split()
        check(zygotes and all(pid.isdecimal() for pid in zygotes), "Cannot find the Android zygote mount namespace")
        for pid in zygotes:
            for path in (ca_path, "/system/etc/security/cacerts"):
                self.shell("nsenter", f"--mount=/proc/{pid}/ns/mnt", "--", "mount", "--bind", target, path)

    def screen(self):
        # Dump and read a file: exec-out's /dev/tty behavior differs by image.
        remote = "/data/local/tmp/ovpn-e2e-window.xml"
        try:
            self.shell("uiautomator", "dump", remote, timeout=15)
            data = self.shell("cat", remote)
            return list(ET.fromstring(data).iter("node"))
        except (ET.ParseError, AssertionError, subprocess.TimeoutExpired):
            return []

    def find(self, label, timeout=30):
        def visible():
            for node in self.screen():
                attrs = node.attrib
                if any(label == attrs.get(key) for key in ("text", "content-desc", "resource-id")):
                    return attrs
            return None
        return until(visible, f"Android shows {label!r}", timeout)

    def tap(self, label, timeout=30):
        self.tap_node(self.find(label, timeout))

    def tap_node(self, node):
        box = [int(n) for n in re.findall(r"\d+", node["bounds"])]
        check(len(box) == 4 and box[2] > box[0] and box[3] > box[1], "Android control has no visible bounds")
        self.shell("input", "tap", (box[0] + box[2]) // 2, (box[1] + box[3]) // 2)

    def launch(self):
        self.shell("am", "start", "-n", ACTIVITY)

    def enter(self, code):
        self.tap("Enter code")
        self.tap("Code or link")
        self.shell("input", "text", code)
        self.tap("Continue")

    def vpn(self):
        for line in self.shell("dumpsys", "connectivity").splitlines():
            if f"VPN:{PACKAGE}" in line and "NetworkAgentInfo" in line:
                network = re.search(r"network\{(\d+)\}", line)
                if network:
                    return network.group(1)
        return None

    def select(self, profile):
        self.tap("Change profile")
        self.tap(profile)


def build_android():
    android_dir = ROOT / "apps/android"
    command(android_dir / "build-core.sh", timeout=360)
    subprocess.run(("./gradlew", "--no-daemon", ":app:testDebugUnitTest", ":app:assembleDebug"),
                   cwd=android_dir, check=True, timeout=360)
    apk = android_dir / "app/build/outputs/apk/debug/app-debug.apk"
    command(android_dir / "check-alignment.sh", apk, timeout=45)
    return apk


def android(lab):
    apk_env = os.environ.get("OVPN_E2E_ANDROID_APK")
    apk = Path(apk_env).resolve() if apk_env else build_android()
    check(apk.is_file(), "The Android test APK has not been built")
    emulator = Emulator()
    directory = lab.directory / "android"
    directory.mkdir(mode=0o700)
    (directory / "bundles").mkdir(mode=0o700)
    (directory / "invites").mkdir(mode=0o700)
    with background(lab.fixture, "android", directory) as fixture:
        ready = directory / "ready"
        def available():
            check(fixture.poll() is None, "Android Xray fixture exited")
            return ready.exists() and ready.stat().st_size > 0
        until(available, "local Xray TLS and REALITY listeners")
        emulator.trust(directory / "root.pem")
        emulator.shell("settings", "put", "global", "private_dns_mode", "off")
        emulator.shell("svc", "power", "stayon", "true")
        emulator.shell("input", "keyevent", "KEYCODE_WAKEUP")
        emulator.shell("wm", "dismiss-keyguard")
        listen_port = port()
        origin = f"https://127.0.0.1:{listen_port}"
        emulator.run("reverse", f"tcp:{listen_port}", f"tcp:{listen_port}")
        try:
            with lab.delivery(directory, listen_port, tls=True):
                bundle_id = ready.read_text()
                invite = ("subscription", "invite", "--bundle-dir", directory / "bundles", "--invite-dir",
                          directory / "invites", "--id", bundle_id, "--base-url", origin)
                code = lab.cli(*invite).strip()
                qr = directory / "setup.png"
                lab.cli(*invite, "--out", qr)
                if emulator.shell("pm", "list", "packages", PACKAGE).strip() == "package:" + PACKAGE:
                    emulator.run("uninstall", PACKAGE)
                emulator.run("install", "-g", apk)
                emulator.shell("pm", "grant", PACKAGE, "android.permission.POST_NOTIFICATIONS")
                emulator.launch()
                onboarding(emulator, code)
                tunnels(emulator, directory)
                replay_and_qr(emulator, code, qr)
                lab.cli("subscription", "revoke", "--bundle-dir", directory / "bundles", "--id", bundle_id)
                emulator.tap("Settings")
                emulator.tap("Manage access")
                emulator.tap("Update list")
                emulator.find("The list is unavailable. Your saved profiles are still here.")
                # Reload persisted access and verify it where the user connects,
                # independently of scrolling on the access management screen.
                emulator.shell("am", "force-stop", PACKAGE)
                emulator.launch()
                emulator.find("Primary")
                emulator.tap("Change profile")
                emulator.find("Backup")
                print("PASS revoked subscription keeps the last usable profile snapshot", flush=True)
        finally:
            emulator.shell("am", "force-stop", PACKAGE)
            emulator.run("reverse", "--remove", f"tcp:{listen_port}")


def onboarding(emulator, code):
    emulator.find("Set up access")
    emulator.enter(code)
    emulator.find("Primary")
    emulator.tap("Connect")
    emulator.tap("android:id/button2")
    emulator.find("Allow the VPN connection in the Android prompt.")
    check(emulator.vpn() is None, "Denying VPN permission created a tunnel")
    print("PASS first access and denied system VPN permission", flush=True)


def tunnels(emulator, directory):
    hits = directory / "probe-hit"
    emulator.tap("Connect")
    emulator.tap("android:id/button1")
    emulator.find("Connected", 60)
    first = until(emulator.vpn, "the real Android VPN network")
    check(hits.stat().st_size > 0, "Connected without reaching the private HTTPS responder through TUN")
    before = hits.stat().st_size
    emulator.select("Backup")
    emulator.find("Backup")
    emulator.find("Connected", 60)
    check(hits.stat().st_size > before, "TLS profile did not reach the private HTTPS responder")
    check(emulator.vpn() == first, "Switching REALITY to TLS replaced the Android VPN network")

    emulator.select("Refused")
    emulator.find("Refused")
    emulator.find("Connection unconfirmed", 65)
    check(emulator.vpn() == first, "Rejected identity removed the VPN route while attempting recovery")
    emulator.tap("Problem report")
    until(lambda: any("Connection report" in node.get("text", "") for node in
                      (item.attrib for item in emulator.screen())), "problem report")
    report = "\n".join(item.get("text", "") for item in (node.attrib for node in emulator.screen()))
    check("Engine:" in report and "Android API:" in report, "Report lacks connection context")
    check(not PRIVATE.search(report) and not re.search(r'https?://|mobile\.fixture\.test|\bRefused\b', report),
          "Report leaked credentials or profile addresses")
    emulator.tap("Close")
    emulator.tap("Disconnect")
    emulator.find("Disconnected")
    until(lambda: emulator.vpn() is None, "VPN network is removed after Disconnect")
    print("PASS private HTTPS through REALITY and TLS, stable TUN, invalid identity and safe report", flush=True)


def replay_and_qr(emulator, code, qr):
    emulator.shell("pm", "clear", PACKAGE)
    emulator.launch()
    emulator.enter(code)
    emulator.find("This code has expired or was already used. Ask for a new one.")
    photo = "/sdcard/Pictures/ovpn-e2e-setup.png"
    emulator.run("push", qr, photo)
    emulator.shell("am", "broadcast", "-a", "android.intent.action.MEDIA_SCANNER_SCAN_FILE", "-d", "file://" + photo)
    emulator.tap("Scan QR code")
    emulator.tap("Choose image")
    def image():
        return next((node.attrib for node in emulator.screen() if
                     node.get("content-desc", "").startswith("Photo taken")), None)
    emulator.tap_node(until(image, "fixture QR image is indexed by the photo picker"))
    emulator.find("Primary")
    emulator.tap("Settings")
    emulator.tap("Русский")
    emulator.find("Язык")
    emulator.tap("简体中文")
    emulator.find("语言")
    emulator.tap("English")
    emulator.find("Language")
    emulator.tap("Back")
    emulator.find("Primary")
    print("PASS one-time code replay rejected, saved QR activated, Russian and Chinese UI", flush=True)
