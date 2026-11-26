import os
from pathlib import Path
import tempfile
import unittest

from ci_scope import FORMULA, git, run_app_checks


class AppCheckScopeTest(unittest.TestCase):
    def setUp(self):
        original = Path.cwd()
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.addCleanup(os.chdir, original)
        os.chdir(directory.name)
        git("init", "-q")
        git("config", "user.name", "CI scope test")
        git("config", "user.email", "test@example.invalid")
        self.base = self.commit(
            {
                "README.md": "# Panora\n",
                "docs/example.md": "# Example\n",
                FORMULA: self.formula("v1.2.3", "a"),
            }
        )

    def commit(self, changes):
        for name, content in changes.items():
            path = Path(name)
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content)
        git("add", "-A")
        git("commit", "-qm", "Fixture")
        return git("rev-parse", "HEAD").decode().strip()

    @staticmethod
    def formula(tag, revision):
        return f'  url "example",\n      tag: "{tag}",\n      revision: "{revision * 40}"\n'

    def test_markdown_only_skips_app_jobs(self):
        head = self.commit({"README.md": "# Updated\n", "docs/example.md": "# New\n"})
        self.assertFalse(run_app_checks(self.base, head))

    def test_mixed_docs_and_source_run_app_jobs(self):
        head = self.commit({"README.md": "# Updated\n", "cmd/panora/main.go": "package main\n"})
        self.assertTrue(run_app_checks(self.base, head))

    def test_non_markdown_docs_fixtures_run_app_jobs(self):
        head = self.commit({"docs/providers/examples/editor/fixture-images.json": "{}\n"})
        self.assertTrue(run_app_checks(self.base, head))

    def test_formula_pin_only_skips_app_jobs(self):
        head = self.commit({FORMULA: self.formula("v1.2.4", "b")})
        self.assertFalse(run_app_checks(self.base, head))

    def test_formula_logic_or_workflow_changes_run_app_jobs(self):
        updated = self.formula("v1.2.4", "b")
        head = self.commit({FORMULA: updated + '  depends_on "go"\n'})
        self.assertTrue(run_app_checks(self.base, head))
        head = self.commit({FORMULA: updated, ".github/workflows/homebrew.yml": "name: Homebrew\n"})
        self.assertTrue(run_app_checks(self.base, head))

    def test_missing_comparison_runs_app_jobs(self):
        self.assertTrue(run_app_checks("0" * 40, self.base))
