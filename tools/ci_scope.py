"""Select the app checks for the tree GitHub is testing."""

import re
import subprocess
import sys

FORMULA = "Formula/panora.rb"
FORMULA_PIN = re.compile(
    rb'^([ \t]+tag:[ \t]+)"v[0-9]+\.[0-9]+\.[0-9]+",\n'
    rb'([ \t]+revision:[ \t]+)"[0-9a-f]{40}"$',
    re.MULTILINE,
)


def git(*args: str) -> bytes:
    return subprocess.check_output(["git", *args])


def tree_exists(revision: str) -> bool:
    return bool(revision) and subprocess.run(
        ["git", "cat-file", "-e", f"{revision}^{{tree}}"], stderr=subprocess.DEVNULL
    ).returncode == 0


def is_formula_bump(base: str, head: str) -> bool:
    entries = [git("ls-tree", revision, "--", FORMULA) for revision in (base, head)]
    if any(not entry.startswith(b"100644 blob ") for entry in entries):
        return False
    before, after = [git("show", f"{revision}:{FORMULA}") for revision in (base, head)]
    old_contents, old_pins = FORMULA_PIN.subn(rb"\1<TAG>,\n\2<REVISION>", before)
    new_contents, new_pins = FORMULA_PIN.subn(rb"\1<TAG>,\n\2<REVISION>", after)
    return (
        old_pins == new_pins == 1
        and before != after
        and old_contents == new_contents
    )


def run_app_checks(base: str, head: str) -> bool:
    if not tree_exists(base):
        return True
    subprocess.run(["git", "diff", "--check", base, head], stdout=sys.stderr, check=True)
    paths = git("diff", "--name-only", "-z", "--no-renames", base, head).split(b"\0")[:-1]
    if paths == [FORMULA.encode()]:
        return not is_formula_bump(base, head)
    return not paths or not all(
        path in (b"README.md", b"CONTRIBUTING.md")
        or (path.startswith(b"docs/") and path.endswith(b".md"))
        for path in paths
    )


if __name__ == "__main__":
    print(f"run_app={str(run_app_checks(*sys.argv[1:])).lower()}")
