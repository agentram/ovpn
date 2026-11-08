"""Run isolated journeys through the built CLI, Desk, or O Tunnel."""
import argparse
from contextlib import contextmanager
import json
import os
from pathlib import Path
import re
import socket
import sqlite3
import subprocess
import tempfile
import time
from urllib.error import HTTPError, URLError
from urllib.request import build_opener, ProxyHandler, Request

ROOT = Path(__file__).resolve().parents[2]
HTTP = build_opener(ProxyHandler({}))
PRIVATE = re.compile(r'(?i)vless://|ot1:|fixture-private-marker|11111111-1111-4111-8111-')
PORT_TIMEOUT = 15


def check(condition, message):
    if not condition:
        raise AssertionError(message)


def command(*args, **kwargs):
    return subprocess.run(args, cwd=ROOT, check=True, **kwargs)


@contextmanager
def background(*args):
    with subprocess.Popen(args, cwd=ROOT, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL) as process:
        try:
            yield process
        finally:
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(10)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()


def until(predicate, label, timeout=PORT_TIMEOUT):
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        value = predicate()
        if value:
            return value
        time.sleep(0.25)
    raise AssertionError(f"Timed out: {label}")


def port():
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        return listener.getsockname()[1]


def response(url):
    try:
        with HTTP.open(Request(url), timeout=3) as res:
            return res.status, res.headers, res.read(1024 * 1024)
    except HTTPError as error:
        with error:
            return error.code, error.headers, b""
    except URLError:
        return 0, {}, b""


class Lab:
    def __init__(self, directory):
        self.directory = directory
        self.worker = directory / "ovpn"
        self.fixture = directory / "fixture"
        self.workspace = directory / "workspace"
        command("go", "build", "-o", self.worker, "./cmd/ovpn", timeout=240)
        command("go", "build", "-o", self.fixture, "./tests/e2e/fixture", timeout=240)

    def cli(self, *args, fail=None, workspace=None, timeout=45):
        result = subprocess.run(
            (self.worker, "--data-dir", workspace or self.workspace, *args),
            cwd=ROOT, capture_output=True, text=True, timeout=timeout,
        )
        if fail is None:
            check(result.returncode == 0, f"CLI failed: {' '.join(args[:2])} (exit {result.returncode})")
        else:
            check(result.returncode != 0 and fail in result.stderr,
                  f"Expected CLI rejection: {' '.join(args[:2])} ({fail})")
        return result.stdout

    @contextmanager
    def delivery(self, directory, listen_port, *, tls=False):
        flags = ("--tls-cert", directory / "leaf.pem", "--tls-key", directory / "leaf.key") if tls else ()
        with background(self.worker, "--data-dir", self.directory / "unused-state",
                        "subscription", "serve", "--listen", f"127.0.0.1:{listen_port}",
                        "--bundle-dir", directory / "bundles", "--invite-dir", directory / "invites", *flags) as process:
            def listening():
                check(process.poll() is None, "Subscription listener exited")
                try:
                    with socket.create_connection(("127.0.0.1", listen_port), timeout=1):
                        return True
                except OSError:
                    return False
            until(listening, "subscription listener")
            yield


def first_server_and_probe(lab):
    new_state = lab.directory / "first-server"
    lab.cli("server", "add", "--name", "First fixture", "--host", "127.0.0.1",
            "--domain", "first.fixture.invalid", "--ssh-user", "root", workspace=new_state)
    inventory = lab.cli("server", "list", workspace=new_state)
    check("First fixture" in inventory, "First server is not listed after registration")

    target = ("--server", "Backup fixture", "--username", "bob")
    checker = lab.directory / "checker-export"
    checker.mkdir(mode=0o700)
    stdout = lab.cli("user", "export", *target, "--format", "xray-checker", "--out", checker)
    config, = checker.glob("*.json")
    raw = config.read_text()
    parsed = json.loads(raw)
    check("inbounds" not in parsed and parsed["outbounds"][0]["streamSettings"]["security"] == "reality",
          "Checker export did not preserve the selected transport")
    check(config.stat().st_mode & 0o777 == 0o600 and "fixture-private-marker" not in raw and not PRIVATE.search(stdout),
          "Checker export exposed credentials or server material")
    output = lab.cli("user", "probe", *target, "--json", "--timeout", "3s", fail="")
    result = json.loads(output)
    check(not result["success"] and result.get("error_code") and not PRIVATE.search(output),
          "Probe accepted the unavailable endpoint or returned unsafe diagnostics")
    print("PASS first-server registration, Checker export and failed native CLI probe", flush=True)


def subscriptions(lab):
    directory = lab.directory / "delivery"
    directory.mkdir(mode=0o700)
    bundles = directory / "bundles"
    bundles.mkdir(mode=0o700)
    (directory / "invites").mkdir(mode=0o700)
    exported = lab.directory / "export.txt"
    common = ("user", "subscription")
    lab.cli(*common, "export", "--username", "alice", "--format", "links", "--out", exported)
    links = exported.read_text().splitlines()
    check(exported.stat().st_mode & 0o777 == 0o600, "Subscription export file is not private")
    check(len(links) == 1 and links[0].startswith("vless://"), "Export included ineligible servers or omitted the eligible server")
    listen_port = port()
    https_origin = f"https://127.0.0.1:{listen_port}"
    publish = (*common, "publish", "--username", "alice", "--bundle-dir", bundles)
    published = lab.cli(*publish, "--base-url", https_origin)
    bundle_id = published.splitlines()[0].removeprefix("subscription id: ")
    url = published.splitlines()[-1].replace("https://", "http://", 1)
    with lab.delivery(directory, listen_port):
        status, headers, data = response(url)
        check(status == 200 and data, "Published URL did not return a snapshot")
        check("no-store" in headers.get("Cache-Control", ""), "Credential response permits caching")
        check(response(url + "invalid")[0] == 404, "Invalid token was accepted")
        lab.cli(*publish, "--id", bundle_id)
        check(response(url)[::2] == (200, data), "Refresh replaced the original URL or changed the snapshot")
        rotated = lab.cli(*publish, "--id", bundle_id, "--base-url", https_origin, "--rotate-token")
        new_url = rotated.splitlines()[-1].replace("https://", "http://", 1)
        check(response(url)[0] == 404 and response(new_url)[0] == 200, "Rotation did not invalidate only the old token")
        lab.cli("subscription", "revoke", "--id", bundle_id, "--bundle-dir", bundles)
        check(response(new_url)[0] == 404, "Revoked delivery is still available")
    check(not (lab.directory / "unused-state").exists(), "Serving subscriptions initialized control state")
    lab.cli(*common, "export", "--username", "alice", "--format", "links", "--out", exported)
    check(exported.read_text().splitlines() == links, "Revoking delivery changed VPN access")
    print("PASS subscription scope, private export, refresh, rotation, revoke", flush=True)


def observer(lab):
    args = ("observe", "policy", "add", "--name", "Fixture", "--server", "Primary fixture",
            "--username", "alice", "--interval", "60", "--daily-limit", "1")
    initial = json.loads(lab.cli("observe", "status"))
    check(not initial["running"] and not initial["policies"], "Observer started without explicit intent")
    lab.cli(*args, "--enable", fail="TRAFFIC")
    policy = json.loads(lab.cli(*args, "--enable", "--confirm-traffic"))
    with background(lab.worker, "--data-dir", lab.workspace, "observe", "run") as process:
        def scheduled():
            check(process.poll() is None, "Observer exited during its scheduled job")
            status = json.loads(lab.cli("observe", "status"))
            return status if status["running"] and any(
                job["kind"] == "scheduled" and job["state"] == "completed" for job in status["jobs"]
            ) else None
        status = until(scheduled, "scheduled observation", 35)
        check(len(status["history"]) == 1 and status["history"][0]["outcome"] != "passed",
              "Scheduled check bypassed the isolated endpoint or produced no evidence")
        check(not PRIVATE.search(json.dumps(status)), "Observation output leaked test credentials")
        lab.cli("observe", "run", fail="IN_USE")
    status = until(lambda: stopped_status(lab), "observer releases ownership")
    check(status["history"] and not any(sample["fresh"] for sample in status["latest"]),
          "Stopped observer presented measurements as fresh")
    lab.cli("observe", "once", policy["id"], "--confirm-traffic", fail="OBSERVATION_NOT_PASSED")
    status = json.loads(lab.cli("observe", "status"))
    check(status["history"][0].get("code") == "DAILY_ATTEMPT_LIMIT", "Observer restart bypassed the daily budget")
    lab.cli("observe", "policy", "pause", policy["id"])
    lab.cli("observe", "policy", "rm", policy["id"])
    check(not json.loads(lab.cli("observe", "status"))["policies"], "Removed policy returned after restart")
    print("PASS observer consent, scheduled failure, exclusive ownership, stale history and budget", flush=True)


def stopped_status(lab):
    status = json.loads(lab.cli("observe", "status"))
    return status if not status["running"] else None


def cli(lab, tools):
    command(lab.fixture, "workspace", lab.workspace, timeout=30)
    db, = lab.workspace.glob("*.db")
    def access_state():
        with sqlite3.connect(f"{db.as_uri()}?mode=ro&immutable=1", uri=True) as conn:
            return conn.execute("SELECT server_id, username, uuid, enabled FROM users ORDER BY server_id, username").fetchall()
    before = access_state()
    first_server_and_probe(lab)
    subscriptions(lab)
    observer(lab)
    check(access_state() == before, "Observing or delivering changed access policy")
    if tools:
        for tool in ("checker", "knife"):
            lab.cli("observe", "install", tool, timeout=180)
        command("go", "test", "-v", "./internal/integrations", "-run",
                "^TestInstalledToolsCarryHTTPSWithoutDirectFallback$", "-count=1",
                env={**os.environ, "OVPN_TEST_TOOL_STATE": str(lab.workspace)}, timeout=180)


def desktop(lab):
    command("npm", "--prefix", "apps/desktop/frontend", "run", "build", timeout=120)
    command("go", "-C", "apps/desktop", "test", "-v", "-run", "^TestDeskBrowserJourneys$", "-count=1",
            env={**os.environ, "OVPN_E2E_WORKER": str(lab.worker), "OVPN_E2E_FIXTURE": str(lab.fixture)}, timeout=260)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("suite", choices=("cli", "desktop", "android", "android-build"))
    parser.add_argument("--tools", action="store_true", help="install and exercise real pinned Checker and Knife binaries")
    args = parser.parse_args()
    if args.suite == "android-build":
        from android import build_android
        build_android()
        return
    with tempfile.TemporaryDirectory(prefix="ovpn-e2e-") as directory:
        lab = Lab(Path(directory))
        if args.suite == "cli":
            cli(lab, args.tools)
        elif args.suite == "desktop":
            desktop(lab)
        else:
            from android import android
            android(lab)


if __name__ == "__main__":
    main()
