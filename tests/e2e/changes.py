"""Select E2E suites from NUL-delimited git diff paths; unknown ranges run all."""
from pathlib import Path
import argparse
import re
import subprocess
import sys
import unittest
import tempfile
from unittest.mock import patch

SUITES = ("cli", "desktop", "android")
SHARED = ("internal/", "mobile/", "pkg/", "cmd/", "tests/e2e/", "scripts/", ".github/actions/", ".github/workflows/")
SHARED_FILES = {"go.mod", "go.sum", "go.work", "go.work.sum", "VERSION", "Makefile", "LICENSE"}


# Distribution follows app sources/contracts, independently of broad integration checks.
DESKTOP_PACKAGE_PREFIXES = (
    "apps/desktop/", "internal/desktop/", "internal/app/",
    "internal/deskmonitor/", "internal/observer/", "internal/observation/",
    "internal/integrations/", "internal/operations/", "internal/probequeue/",
    "internal/cli/desk_",
    "internal/machine/", "internal/haproxy/", "internal/ha/", "internal/runtimeassets/",
    "internal/version/", "internal/util/",
)
DESKTOP_PACKAGE_FILES = {
    "go.mod", "go.sum", "go.work", "go.work.sum", "LICENSE", "internal/cli/operation_dispatch.go",
    "internal/store/local/store_migrate.go",
    "scripts/build_desktop.py", "scripts/build_desktop_icons.py",
    "scripts/package_desktop.py", "scripts/test_package_desktop.py",
    "tests/e2e/changes.py", ".github/workflows/desktop.yml", ".github/workflows/e2e.yml",
}


def desktop_package(paths):
    return any(path.startswith(DESKTOP_PACKAGE_PREFIXES) or path in DESKTOP_PACKAGE_FILES for path in paths)


def selected(paths):
    result = set()
    for path in paths:
        if path.startswith(SHARED) or path in SHARED_FILES:
            return set(SUITES)
        if path.startswith("apps/desktop/"):
            result.add("desktop")
        elif path.startswith("apps/android/"):
            result.add("android")
        elif path.startswith(("ansible/", "examples/", "docker-compose")):
            result.add("cli")
    return result


def changed_paths(event, base, head):
    if event not in {"push", "pull_request", "merge_group"}:
        return None
    if any(not re.fullmatch(r"[0-9a-f]{40,64}", rev) or not rev.strip("0") for rev in (base, head)):
        return None
    comparison = f"{base}{'...' if event == 'pull_request' else '..'}{head}"
    diff = subprocess.run(("git", "diff", "--no-renames", "--name-only", "-z", comparison, "--"), capture_output=True)
    if diff.returncode != 0:
        return None
    return list(filter(None, diff.stdout.decode("utf-8", errors="surrogateescape").split("\0")))


def changed(event, base, head):
    paths = changed_paths(event, base, head)
    return set(SUITES) if paths is None else selected(paths)


class SelectionTests(unittest.TestCase):
    def test_app_changes_run_only_the_affected_apps(self):
        self.assertEqual({"desktop"}, selected(["apps/desktop/frontend/src/main.tsx"]))
        self.assertEqual({"android"}, selected(["apps/android/gradle/libs.versions.toml"]))
        self.assertEqual({"desktop", "android"}, selected(["apps/desktop/go.sum", "apps/android/build-core.sh"]))

    def test_shared_code_dependencies_and_runner_changes_run_every_suite(self):
        for path in ("internal/desktop/protocol.go", "internal/integrations/runner.go",
                     "mobile/androidbridge/tunnel_android.go", "go.sum", ".github/workflows/e2e.yml"):
            with self.subTest(path=path):
                self.assertEqual(set(SUITES), selected([path]))

    def test_docs_only_has_no_runtime_jobs_and_unknown_ranges_run_all(self):
        self.assertEqual(set(), selected(["docs/testing.md", "README.md", "CHANGELOG.md"]))
        self.assertEqual(set(SUITES), changed("workflow_dispatch", "", ""))
        self.assertEqual(set(SUITES), changed("push", "0" * 40, "b" * 40))


class DesktopPackageSelectionTests(unittest.TestCase):
    def test_app_contracts_dependencies_and_packaging_trigger_distribution(self):
        for path in ("apps/desktop/frontend/src/desk/App.tsx", "apps/desktop/main.go",
                     "apps/desktop/go.sum", "internal/desktop/worker.go", "internal/app/dto/views.go",
                     "internal/machine/control.go", "internal/operations/api/types.go", "internal/haproxy/api/types.go",
                     "internal/app/users.go", "internal/deskmonitor/controller.go", "internal/integrations/runner.go", "internal/cli/desk_control.go",
                     "go.mod", "go.sum", "scripts/package_desktop.py", ".github/workflows/desktop.yml"):
            with self.subTest(path=path):
                self.assertTrue(desktop_package([path]))

    def test_cli_server_android_docs_and_version_only_do_not_publish_desk(self):
        paths = ["internal/cli/server_cmd.go", "internal/deploy/bundle.go",
                 "cmd/ovpn-agent/main.go", "apps/android/app/build.gradle.kts", "ansible/playbooks/bootstrap.yml",
                 "docs/desktop.md", "README.md", "CHANGELOG.md", "VERSION", ".github/workflows/security.yml"]
        self.assertFalse(desktop_package(paths))
        self.assertEqual(set(SUITES), selected(paths))
        self.assertTrue(desktop_package(paths + ["apps/desktop/frontend/src/desk/App.tsx"]))

    def test_local_host_imports_cannot_silently_escape_selection(self):
        root = Path(__file__).resolve().parents[2]
        pending = [root / "apps/desktop"]
        seen = set()
        while pending:
            directory = pending.pop()
            if directory in seen:
                continue
            seen.add(directory)
            for source in directory.glob("*.go"):
                if source.name.endswith("_test.go"):
                    continue
                for package in re.findall(r'"ovpn/([^"\n]+)"', source.read_text()):
                    self.assertTrue(desktop_package([package + "/implementation.go"]), package)
                    pending.append(root / package)


class DiffSelectionTests(unittest.TestCase):
    def test_real_git_diffs_include_renames_and_merge_queue_changes(self):
        with tempfile.TemporaryDirectory(prefix="ovpn-selection-") as directory:
            root = Path(directory)
            def git(*args):
                return subprocess.check_output(("git", "-C", directory, *args), text=True, stderr=subprocess.DEVNULL).strip()
            git("init", "-q")
            git("config", "user.name", "Fixture")
            git("config", "user.email", "fixture@example.test")
            def commit(path):
                target = root / path
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_text("fixture\n")
                git("add", "--all")
                git("commit", "-qm", "Fixture change")
                return git("rev-parse", "HEAD")
            base = commit("README.md")
            cli = commit("internal/cli/server_cmd.go")
            real_run = subprocess.run
            def in_fixture(*args, **kwargs):
                kwargs.setdefault("cwd", directory)
                return real_run(*args, **kwargs)
            with patch("subprocess.run", side_effect=in_fixture):
                paths = changed_paths("pull_request", base, cli)
                self.assertFalse(desktop_package(paths))
                app = commit("apps/desktop/frontend/src/desk/App.tsx")
                self.assertTrue(desktop_package(changed_paths("merge_group", cli, app)))
                git("mv", "apps/desktop/frontend/src/desk/App.tsx", "renamed-file.txt")
                git("commit", "-qm", "Move app source")
                moved = git("rev-parse", "HEAD")
                self.assertTrue(desktop_package(changed_paths("push", app, moved)))
                self.assertIsNone(changed_paths("push", "0" * 40, moved))
                self.assertIsNone(changed_paths("workflow_dispatch", "", ""))


if __name__ == "__main__":
    if sys.argv[1:] == ["--test"]:
        unittest.main(argv=[sys.argv[0]])
    else:
        parser = argparse.ArgumentParser(description=__doc__)
        parser.add_argument("--event", required=True)
        parser.add_argument("--base", default="")
        parser.add_argument("--head", default="")
        parser.add_argument("--output", required=True, type=Path)
        args = parser.parse_args()
        paths = changed_paths(args.event, args.base, args.head)
        suites = set(SUITES) if paths is None else selected(paths)
        package = paths is None or desktop_package(paths)
        with args.output.open("a") as output:
            for suite in SUITES:
                print(f"{suite}={str(suite in suites).lower()}", file=output)
            print(f"desktop_package={str(package).lower()}", file=output)
        print("Desktop packages:", "build" if package else "skip")
        print("Selected suites:", ", ".join(sorted(suites)) or "none")
