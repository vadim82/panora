import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import homebrew_update as updater


class HomebrewUpdateTests(unittest.TestCase):
    tag = "v1.2.4"
    before = b'  url "source",\n      tag: "v1.2.3",\n      revision: "' + b"a" * 40 + b'"\n'

    def setUp(self):
        original = Path.cwd()
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.addCleanup(os.chdir, original)
        os.chdir(temporary.name)
        self.git("init", "-b", "main")
        self.git("config", "user.name", "Test")
        self.git("config", "user.email", "test@example.com")
        formula = Path(updater.FORMULA)
        formula.parent.mkdir()
        formula.write_bytes(self.before)
        self.base = self.commit()
        self.revision = self.base
        self.git("checkout", "-b", updater.BRANCH + self.tag)
        formula.write_bytes(updater.formula_update(self.before, self.tag, self.revision))
        self.head = self.commit()
        self.git("checkout", "main")
        self.git("remote", "add", "origin", ".")
        self.git("update-ref", "refs/pull/42/head", self.head)
        self.pr = {
            "number": 42,
            "state": "open",
            "html_url": "https://github.com/test/panora/pull/42",
            "user": {"login": "test-updater[bot]", "type": "Bot"},
            "head": {
                "ref": updater.BRANCH + self.tag,
                "sha": self.head,
                "repo": {"full_name": "test/panora"},
            },
            "base": {"ref": "main", "repo": {"full_name": "test/panora"}},
        }
        self.go_jobs = [self.job(name, "skipped") for name in updater.GO_JOBS]
        self.go_jobs.append(self.job("Determine app check scope"))
        self.brew_jobs = [self.job("install"), self.job("archive")]
        self.reviews = [[]]
        self.merge = None
        self.stable_tag = self.tag
        environment = patch.dict(os.environ, {
            "APP_SLUG": "test-updater",
            "APP_TOKEN": "test-token",
            "EXPECTED_HEAD": self.head,
            "GITHUB_REPOSITORY": "test/panora",
            "GITHUB_REPOSITORY_OWNER": "test",
            "GITHUB_STEP_SUMMARY": "summary.md",
            "RELEASE_TAG": self.tag,
            "TOKEN_APP_SLUG": "test-updater",
            "UPDATE_ACTION": "merge",
        })
        environment.start()
        self.addCleanup(environment.stop)

    @staticmethod
    def git(*args):
        return updater.git("-c", "advice.detachedHead=false", *args)

    def commit(self):
        self.git("add", "-A")
        self.git("commit", "-qm", "Fixture")
        return self.git("rev-parse", "HEAD").decode().strip()

    def job(self, name, conclusion="success"):
        return dict(name=name, status="completed", conclusion=conclusion, head_sha=self.head)

    def api(self, endpoint, *args):
        if endpoint == "releases?per_page=100":
            return [[{"tag_name": self.stable_tag, "draft": False, "prerelease": False}]]
        if endpoint == f"commits/refs/tags/{self.tag}":
            return {"sha": self.revision}
        if endpoint == "pulls":
            return [self.pr]
        if endpoint.startswith("actions/workflows/"):
            workflow = endpoint.split("/")[2]
            self.assertIn(f"head_sha={self.pr['head']['sha']}", args)
            self.assertIn("event=pull_request", args)
            return {"workflow_runs": [{
                "id": 1 if workflow == "go.yml" else 2,
                "run_attempt": 3,
                "head_repository": {"full_name": "test/panora"},
                "head_sha": self.pr["head"]["sha"],
                "head_branch": self.pr["head"]["ref"],
                "event": "pull_request",
                "status": "completed",
                "conclusion": "success",
                "html_url": "https://github.com/test/panora/actions",
            }]}
        if endpoint == "actions/runs/1/attempts/3/jobs":
            return {"jobs": self.go_jobs}
        if endpoint == "actions/runs/2/attempts/3/jobs":
            return {"jobs": self.brew_jobs}
        if endpoint == "pulls/42/reviews?per_page=100":
            return self.reviews
        if endpoint == "pulls/42/merge":
            self.merge = args
            return {"merged": True}
        self.fail(f"Unexpected GitHub endpoint: {endpoint}")

    def run_update(self):
        with patch.object(updater, "api", self.api), patch.object(
            updater, "gh", return_value=json.dumps({"reviewDecision": "", "mergeStateStatus": "CLEAN"})
        ):
            updater.apply()
        return Path("summary.md").read_text()

    def test_generated_update_merges_with_verified_sha(self):
        summary = self.run_update()
        self.assertIn(f"sha={self.head}", self.merge)
        self.assertIn("Reusing the existing PR", summary)
        self.assertIn("Merged formula PR #42", summary)

    def test_missing_required_check_blocks_merge(self):
        self.go_jobs.pop(0)
        self.assertIn("The Native client frontend check is missing", self.run_update())
        self.assertIsNone(self.merge)

    def test_failed_required_check_blocks_merge(self):
        self.go_jobs[0]["conclusion"] = "failure"
        self.assertIn("The Native client frontend check did not pass", self.run_update())
        self.assertIsNone(self.merge)

    def test_failed_homebrew_check_blocks_merge(self):
        self.brew_jobs[0]["conclusion"] = "failure"
        self.assertIn("The install check did not pass", self.run_update())
        self.assertIsNone(self.merge)

    def test_checks_for_another_head_block_merge(self):
        self.brew_jobs[1]["head_sha"] = self.base
        self.assertIn("The archive check is missing", self.run_update())
        self.assertIsNone(self.merge)

    def test_unexpected_formula_edit_blocks_merge(self):
        self.git("checkout", updater.BRANCH + self.tag)
        with open(updater.FORMULA, "ab") as formula:
            formula.write(b"# Extra change\n")
        altered = self.commit()
        self.git("update-ref", "refs/pull/42/head", altered)
        self.pr["head"]["sha"] = altered
        self.git("checkout", "main")
        self.assertIn("The diff differs from the generated tag and revision update", self.run_update())
        self.assertIsNone(self.merge)

    def test_requested_changes_block_merge(self):
        self.reviews = [[{"user": {"login": "reviewer"}, "state": "CHANGES_REQUESTED"}]]
        self.assertIn("A reviewer has requested changes", self.run_update())
        self.assertIsNone(self.merge)

    def test_different_verified_head_blocks_merge(self):
        with patch.dict(os.environ, {"EXPECTED_HEAD": self.base}):
            self.assertIn("The PR head changed before merge", self.run_update())
        self.assertIsNone(self.merge)

    def test_older_release_blocks_merge(self):
        self.stable_tag = "v1.3.0"
        with patch.object(updater, "released_versions", return_value={self.tag, self.stable_tag}):
            self.assertIn(
                f"{self.tag} is older than the published stable release {self.stable_tag}",
                self.run_update(),
            )
        self.assertIsNone(self.merge)
