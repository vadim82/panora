"""Create and merge release-generated formula PRs from trusted main."""

import json
import os
import re
import subprocess
import sys
from pathlib import Path

from ci_scope import FORMULA, FORMULA_PIN, git, is_formula_bump

TAG = r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)"
BRANCH = "automation/homebrew-"
ACTIONS_BOT = "github-actions[bot]"
GO_JOBS = (
    "Native client frontend",
    "Lint (CLI and service)",
    "Lint (Native bridge)",
    "Test (ubuntu-latest)",
    "Test (macos-latest)",
)


def gh(*args):
    return subprocess.check_output(["gh", *args])


def api(endpoint, *args):
    return json.loads(gh("api", f"repos/{os.environ['GITHUB_REPOSITORY']}/{endpoint}", *args))


def version(tag):
    match = re.fullmatch(TAG, tag)
    if not match:
        raise ValueError("Expected a stable release tag in vX.Y.Z form.")
    return tuple(map(int, match.groups()))


def formula_pin(contents):
    pins = list(FORMULA_PIN.finditer(contents))
    if len(pins) != 1:
        raise ValueError("Expected one tag and revision in Formula/panora.rb.")
    return pins[0]


def formula_update(contents, tag, revision):
    pin = formula_pin(contents)
    return (
        contents[: pin.start()]
        + pin[1] + f'"{tag}",\n'.encode()
        + pin[2] + f'"{revision}"'.encode()
        + contents[pin.end() :]
    )


def head_matches(base, head, tag, revision):
    fork = git("merge-base", base, head).decode().strip()
    paths = git("diff", "--name-only", "-z", "--no-renames", fork, head)
    if paths != FORMULA.encode() + b"\0" or not is_formula_bump(fork, head):
        return False
    expected = formula_update(git("show", f"{fork}:{FORMULA}"), tag, revision)
    # The generated patch must also apply exactly to the formula on current main.
    return (
        git("show", f"{head}:{FORMULA}") == expected
        and formula_update(git("show", f"{base}:{FORMULA}"), tag, revision) == expected
    )


def report(message):
    print(message)
    with open(os.environ["GITHUB_STEP_SUMMARY"], "a", encoding="utf-8") as summary:
        summary.write(message + "\n\n")


def released_versions():
    pages = api("releases?per_page=100", "--paginate", "--slurp")
    return {
        r["tag_name"]
        for page in pages
        for r in page
        if not r["draft"] and not r["prerelease"] and re.fullmatch(TAG, r["tag_name"])
    }


def release_revision(tag):
    stable = released_versions()
    if tag not in stable:
        report(f"{tag} is not a published stable release.")
        return None
    latest = max(stable, key=version)
    if tag != latest:
        report(f"{tag} is older than the published stable release {latest}.")
        return None
    sha = api(f"commits/refs/tags/{tag}")["sha"]
    if not re.fullmatch(r"[0-9a-f]{40}", sha):
        raise ValueError("GitHub did not resolve the release tag to a commit.")
    if subprocess.run(["git", "merge-base", "--is-ancestor", sha, "HEAD"]).returncode != 0:
        report(f"The release commit for {tag} is outside the history of main.")
        return None
    return sha


def find_pr(tag):
    owner = os.environ["GITHUB_REPOSITORY_OWNER"]
    prs = api(
        "pulls", "--method", "GET",
        "-f", "state=all", "-f", f"head={owner}:{BRANCH}{tag}", "-f", "base=main",
    )
    return next((pr for pr in prs if pr["state"] == "open"), prs[0] if prs else None)


def pull_request_problem(pr, branch, tag, revision):
    bots = {ACTIONS_BOT, f"{os.getenv('APP_SLUG', '')}[bot]"}
    repo = os.environ["GITHUB_REPOSITORY"]
    if (
        pr["user"]["type"] != "Bot"
        or pr["user"]["login"] not in bots
        or (pr["head"]["repo"] or {}).get("full_name") != repo
        or pr["head"]["ref"] != branch
        or (pr["base"]["repo"] or {}).get("full_name") != repo
        or pr["base"]["ref"] != "main"
    ):
        return "The PR was not created by the release workflow for this repository and tag."
    git("fetch", "--no-tags", "origin", f"refs/pull/{pr['number']}/head")
    if git("rev-parse", "FETCH_HEAD").decode().strip() != pr["head"]["sha"]:
        return "The PR head changed during inspection."
    if not head_matches("HEAD", pr["head"]["sha"], tag, revision):
        return "The diff differs from the generated tag and revision update."
    return None


def check_problem(run, jobs, expected, head):
    if run["status"] != "completed":
        return f"The workflow is {run['status'].replace('_', ' ')}."
    if run["conclusion"] != "success":
        return f"The workflow concluded with {run['conclusion']}."
    for name, allowed in expected.items():
        matches = [job for job in jobs if job["name"] == name and job["head_sha"] == head]
        if len(matches) != 1:
            return f"The {name} check is missing from the current attempt."
        job = matches[0]
        if job["status"] != "completed" or job["conclusion"] not in allowed:
            return f"The {name} check did not pass."
    return None


def verification_problem(pr):
    branch = pr["head"]["ref"]
    head = pr["head"]["sha"]
    go_checks = {name: {"success", "skipped", "neutral"} for name in GO_JOBS}
    go_checks["Determine app check scope"] = {"success"}
    brew_checks = {"install": {"success"}, "archive": {"success"}}
    for workflow, expected in (("go.yml", go_checks), ("homebrew.yml", brew_checks)):
        runs = api(
            f"actions/workflows/{workflow}/runs", "--method", "GET",
            "-f", "event=pull_request", "-f", f"head_sha={head}", "-f", f"branch={branch}",
            "-f", "per_page=1",
        )["workflow_runs"]
        if not runs:
            return f"No {workflow} pull-request workflow exists for {head}."
        run = runs[0]
        if (
            (run["head_repository"] or {}).get("full_name") != os.environ["GITHUB_REPOSITORY"]
            or run["head_sha"] != head
            or run["head_branch"] != branch
            or run["event"] != "pull_request"
        ):
            return f"The {workflow} workflow does not match this PR head."
        jobs = api(f"actions/runs/{run['id']}/attempts/{run['run_attempt']}/jobs")["jobs"]
        problem = check_problem(run, jobs, expected, head)
        if problem:
            return f"{workflow} {problem} [Run details]({run['html_url']})."

    reviews = api(f"pulls/{pr['number']}/reviews?per_page=100", "--paginate", "--slurp")
    decisive = {}
    for page in reviews:
        for review in page:
            if review["user"] and review["state"] in {"APPROVED", "CHANGES_REQUESTED", "DISMISSED"}:
                decisive[review["user"]["login"]] = review["state"]
    if "CHANGES_REQUESTED" in decisive.values():
        return "A reviewer has requested changes."

    status = json.loads(gh(
        "pr", "view", str(pr["number"]), "--repo", os.environ["GITHUB_REPOSITORY"],
        "--json", "reviewDecision,mergeStateStatus",
    ))
    if status["reviewDecision"] in {"REVIEW_REQUIRED", "CHANGES_REQUESTED"}:
        return f"GitHub reports {status['reviewDecision'].lower().replace('_', ' ')}."
    if status["mergeStateStatus"] != "CLEAN":
        return f"GitHub reports merge state {status['mergeStateStatus'].lower()}."
    return None


def inspect(tag, allow_open):
    version(tag)
    revision = release_revision(tag)
    if not revision:
        return None
    branch = BRANCH + tag
    formula = Path(FORMULA).read_bytes()
    current = re.search(rb'"(v[0-9.]+)"', formula_pin(formula)[0])[1].decode()
    if version(current) >= version(tag):
        report(f"The formula already tracks Panora {current}.")
        return None
    pr = find_pr(tag)
    if not pr:
        return ("open", revision, "") if allow_open else None
    url = pr["html_url"]
    if pr["state"] != "open":
        report(f"The existing PR for {tag} is closed. [Formula PR]({url}).")
        return None
    report(f"Reusing the existing PR for {tag}. [Formula PR]({url}).")
    problem = pull_request_problem(pr, branch, tag, revision) or verification_problem(pr)
    if problem:
        report(problem)
        return None
    return ("merge", pr["number"], pr["head"]["sha"])


def create_pr(tag, revision, use_app):
    branch = BRANCH + tag
    if git("ls-remote", "origin", f"refs/heads/{branch}"):
        git("fetch", "--no-tags", "origin", f"refs/heads/{branch}")
        if not head_matches("HEAD", "FETCH_HEAD", tag, revision):
            report(f"The existing {branch} branch contains a different change.")
            return
    else:
        formula = Path(FORMULA)
        formula.write_bytes(formula_update(formula.read_bytes(), tag, revision))
        subprocess.run(["ruby", "-c", FORMULA], check=True)
        git("add", "--", FORMULA)
        git("diff", "--cached", "--check")
        bot = f"{os.environ['APP_SLUG']}[bot]" if use_app else ACTIONS_BOT
        user_id = json.loads(gh("api", f"users/{bot.replace('[', '%5B').replace(']', '%5D')}"))["id"]
        git("config", "user.name", bot)
        git("config", "user.email", f"{user_id}+{bot}@users.noreply.github.com")
        gh("auth", "setup-git")
        git("commit", "-m", f"Update Homebrew formula to Panora {tag}")
        git("push", "origin", f"HEAD:refs/heads/{branch}")
    body = f"The Homebrew formula now tracks Panora {tag} and its exact release commit."
    if not use_app:
        body += (
            "\n\nApprove the workflow runs and merge the verified PR. "
            "Configure the release App in docs/releases.md to automate these steps."
        )
    repo = os.environ["GITHUB_REPOSITORY"]
    body += f"\n\n[Homebrew formula update runs](https://github.com/{repo}/actions/workflows/homebrew-update.yml)"
    url = api(
        "pulls", "--method", "POST", "-f", "base=main", "-f", f"head={branch}",
        "-f", f"title=Update Homebrew formula to Panora {tag}", "-f", f"body={body}",
    )["html_url"]
    report(f"Opened the formula update. [Formula PR]({url}).")


def merge_pr(number, head):
    result = api(f"pulls/{number}/merge", "--method", "PUT", "-f", f"sha={head}", "-f", "merge_method=merge")
    if not result["merged"]:
        raise ValueError("GitHub declined to merge the verified PR.")
    report(f"Merged formula PR #{number} at verified head {head}.")


def plan():
    event = os.environ["GITHUB_EVENT_NAME"]
    branch = os.getenv("RUN_BRANCH", "")
    tag = os.getenv("RELEASE_TAG", "")
    if event == "schedule":
        stable = released_versions()
        if not stable:
            return
        tag = max(stable, key=version)
    elif event == "workflow_run":
        if not branch.startswith(BRANCH) or not re.fullmatch(TAG, branch.removeprefix(BRANCH)):
            return
        tag = branch.removeprefix(BRANCH)

    result = inspect(tag, allow_open=event in {"release", "workflow_dispatch"})
    if result:
        action, _, head = result
        with open(os.environ["GITHUB_OUTPUT"], "a", encoding="utf-8") as output:
            output.write(f"action={action}\ntag={tag}\nhead={head}\n")


def apply():
    action = os.environ["UPDATE_ACTION"]
    tag = os.environ["RELEASE_TAG"]
    token = os.getenv("APP_TOKEN")
    if token and os.environ["TOKEN_APP_SLUG"] != os.environ["APP_SLUG"]:
        raise ValueError("The configured Homebrew App slug does not match its credential.")

    # Refresh main and repeat every gate after planning and before selecting the
    # write credential. GitHub also checks branch protection and the supplied SHA.
    git("fetch", "--no-tags", "origin", "refs/heads/main")
    git("checkout", "--detach", "FETCH_HEAD")
    result = inspect(tag, allow_open=action == "open")
    if not result or result[0] != action:
        report("The release or PR changed before the update could be applied.")
        return
    _, value, head = result
    if token:
        os.environ["GH_TOKEN"] = token
    if action == "open":
        create_pr(tag, value, bool(token))
    elif token and head == os.environ["EXPECTED_HEAD"]:
        merge_pr(value, head)
    else:
        report("The PR head changed before merge.")


if __name__ == "__main__":
    try:
        {"plan": plan, "apply": apply}[sys.argv[1]]()
    except (ValueError, subprocess.CalledProcessError) as error:
        report(str(error))
        sys.exit(1)
