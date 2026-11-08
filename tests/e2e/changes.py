"""Select E2E suites from NUL-delimited git diff paths; unknown ranges run all."""
from pathlib import Path
import argparse
import re
import subprocess
import sys
import unittest

SUITES = ("cli", "desktop", "android")
SHARED = ("internal/", "mobile/", "pkg/", "cmd/", "tests/e2e/", "scripts/", ".github/actions/", ".github/workflows/")
SHARED_FILES = {"go.mod", "go.sum", "go.work", "go.work.sum", "VERSION", "Makefile", "LICENSE"}


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


def changed(event, base, head):
    if event not in {"push", "pull_request"}:
        return set(SUITES)
    if any(not re.fullmatch(r"[0-9a-f]{40,64}", rev) or not rev.strip("0") for rev in (base, head)):
        return set(SUITES)
    comparison = f"{base}{'...' if event == 'pull_request' else '..'}{head}"
    diff = subprocess.run(("git", "diff", "--no-renames", "--name-only", "-z", comparison, "--"),
                          capture_output=True)
    if diff.returncode != 0:
        return set(SUITES)
    return selected(filter(None, diff.stdout.decode("utf-8", errors="surrogateescape").split("\0")))


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
        suites = changed(args.event, args.base, args.head)
        with args.output.open("a") as output:
            for suite in SUITES:
                print(f"{suite}={str(suite in suites).lower()}", file=output)
        print("Selected suites:", ", ".join(sorted(suites)) or "none")
