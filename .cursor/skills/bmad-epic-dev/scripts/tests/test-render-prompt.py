#!/usr/bin/env python3
# /// script
# requires-python = ">=3.9"
# dependencies = []
# ///
"""Unit tests for render-prompt.py."""

from __future__ import annotations

import os
import sys
import tempfile
import unittest
from io import StringIO
from pathlib import Path
from unittest.mock import patch

# Add parent directory to path so we can import the module
sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

# Import after path manipulation
import importlib

render_prompt = importlib.import_module("render-prompt")


class TestCreateStoryTemplate(unittest.TestCase):
    """Tests for the create-story template."""

    def test_full_substitution(self) -> None:
        variables = {
            "project_root": "/my/project",
            "epic_num": "5",
            "story_num": "3",
            "title": "Add login page",
        }
        result = render_prompt.render("create-story", variables)
        self.assertIn("/my/project/_bmad/custom/bmad-create-story.toml", result)
        self.assertIn("story 5.3 in epic 5", result)
        self.assertIn('"Add login page"', result)
        self.assertNotIn("{project_root}", result)
        self.assertNotIn("{epic_num}", result)
        self.assertNotIn("{story_num}", result)
        self.assertNotIn("{title}", result)


class TestDevStoryTemplate(unittest.TestCase):
    """Tests for the dev-story template."""

    def test_full_substitution(self) -> None:
        variables = {
            "project_root": "/root",
            "epic_num": "10",
            "story_num": "2",
            "story_key": "10-2-auth",
            "title": "Implement auth",
            "implementation_artifacts": "/root/_bmad-output/impl",
        }
        result = render_prompt.render("dev-story", variables)
        self.assertIn("/root/_bmad/custom/bmad-dev-story.toml", result)
        self.assertIn("story 10.2 for epic 10", result)
        self.assertIn("/root/_bmad-output/impl/10-2-auth.md", result)
        self.assertIn('feat(epic-10): 10-2-auth — Implement auth', result)
        self.assertNotIn("{project_root}", result)
        self.assertNotIn("{story_key}", result)


class TestCodeReviewTemplate(unittest.TestCase):
    """Tests for the code-review template."""

    def test_full_substitution(self) -> None:
        variables = {
            "project_root": "/proj",
            "story_key": "7-1-api",
            "story_spec_content": "## Story Spec\nDo the thing.",
            "diff_output": "diff --git a/foo.go b/foo.go\n+new line",
        }
        result = render_prompt.render("code-review", variables)
        self.assertIn("/proj/_bmad/custom/bmad-code-review.toml", result)
        self.assertIn("story 7-1-api", result)
        self.assertIn("<spec>\n## Story Spec\nDo the thing.\n</spec>", result)
        self.assertIn("<diff>\ndiff --git a/foo.go b/foo.go\n+new line\n</diff>", result)

    def test_spec_file_arg(self) -> None:
        with tempfile.NamedTemporaryFile(
            mode="w", suffix=".md", delete=False
        ) as f:
            f.write("# My Spec\nDetails here.")
            spec_path = f.name
        try:
            _, variables = render_prompt.parse_args(
                [
                    "code-review",
                    f"--project_root=/proj",
                    f"--story_key=7-1-api",
                    f"--spec-file={spec_path}",
                    "--diff_output=some diff",
                ]
            )
            self.assertEqual(variables["story_spec_content"], "# My Spec\nDetails here.")
        finally:
            os.unlink(spec_path)

    def test_diff_file_arg(self) -> None:
        with tempfile.NamedTemporaryFile(
            mode="w", suffix=".diff", delete=False
        ) as f:
            f.write("--- a/file\n+++ b/file\n+added")
            diff_path = f.name
        try:
            _, variables = render_prompt.parse_args(
                [
                    "code-review",
                    "--project_root=/proj",
                    "--story_key=7-1-api",
                    "--story_spec_content=spec",
                    f"--diff-file={diff_path}",
                ]
            )
            self.assertEqual(
                variables["diff_output"], "--- a/file\n+++ b/file\n+added"
            )
        finally:
            os.unlink(diff_path)


class TestFixPassTemplate(unittest.TestCase):
    """Tests for the fix-pass template."""

    def test_full_substitution(self) -> None:
        variables = {
            "project_root": "/proj",
            "epic_num": "3",
            "story_key": "3-4-refactor",
            "review_findings": "- Must fix: null check\n- Should fix: naming",
        }
        result = render_prompt.render("fix-pass", variables)
        self.assertIn("/proj/_bmad/custom/bmad-dev-story.toml", result)
        self.assertIn("story 3-4-refactor in epic 3", result)
        self.assertIn("epic-3/story-3-4-refactor", result)
        self.assertIn(
            "<findings>\n- Must fix: null check\n- Should fix: naming\n</findings>",
            result,
        )
        self.assertIn(
            'fix(epic-3): 3-4-refactor — address review findings', result
        )


class TestMissingRequiredKeys(unittest.TestCase):
    """Missing required keys produce warnings to stderr."""

    def test_create_story_missing_keys(self) -> None:
        stderr = StringIO()
        with patch("sys.stderr", stderr):
            result = render_prompt.render("create-story", {"project_root": "/r"})
        warnings = stderr.getvalue()
        self.assertIn("epic_num", warnings)
        self.assertIn("story_num", warnings)
        self.assertIn("title", warnings)
        self.assertNotIn("project_root", warnings)
        # Unsubstituted placeholders remain
        self.assertIn("{epic_num}", result)
        self.assertIn("{story_num}", result)
        self.assertIn("{title}", result)

    def test_dev_story_missing_keys(self) -> None:
        stderr = StringIO()
        with patch("sys.stderr", stderr):
            render_prompt.render("dev-story", {})
        warnings = stderr.getvalue()
        for key in render_prompt.REQUIRED_KEYS["dev-story"]:
            self.assertIn(key, warnings)

    def test_code_review_missing_keys(self) -> None:
        stderr = StringIO()
        with patch("sys.stderr", stderr):
            render_prompt.render("code-review", {})
        warnings = stderr.getvalue()
        for key in render_prompt.REQUIRED_KEYS["code-review"]:
            self.assertIn(key, warnings)

    def test_fix_pass_missing_keys(self) -> None:
        stderr = StringIO()
        with patch("sys.stderr", stderr):
            render_prompt.render("fix-pass", {})
        warnings = stderr.getvalue()
        for key in render_prompt.REQUIRED_KEYS["fix-pass"]:
            self.assertIn(key, warnings)


class TestUnknownTemplate(unittest.TestCase):
    """Unknown template name exits with error."""

    def test_unknown_template_exits(self) -> None:
        with self.assertRaises(SystemExit) as ctx:
            stderr = StringIO()
            with patch("sys.stderr", stderr):
                render_prompt.render("nonexistent", {})
        self.assertEqual(ctx.exception.code, 1)

    def test_unknown_template_error_message(self) -> None:
        stderr = StringIO()
        with self.assertRaises(SystemExit):
            with patch("sys.stderr", stderr):
                render_prompt.render("bad-name", {})
        self.assertIn("unknown template", stderr.getvalue())


class TestUnprovidedVarsLeftAsIs(unittest.TestCase):
    """Template variables not provided are left as-is."""

    def test_platform_vars_preserved(self) -> None:
        result = render_prompt.render(
            "dev-story",
            {
                "project_root": "/proj",
                "epic_num": "1",
                "story_num": "1",
                "story_key": "1-1-init",
                "title": "Init",
                # deliberately omit implementation_artifacts
            },
        )
        # {implementation_artifacts} should remain untouched
        self.assertIn("{implementation_artifacts}", result)

    def test_extra_vars_ignored(self) -> None:
        variables = {
            "project_root": "/proj",
            "epic_num": "1",
            "story_num": "1",
            "title": "T",
            "extra_key": "extra_val",
        }
        result = render_prompt.render("create-story", variables)
        self.assertNotIn("extra_val", result)


class TestParseArgs(unittest.TestCase):
    """Tests for CLI argument parsing."""

    def test_no_args_exits(self) -> None:
        with self.assertRaises(SystemExit):
            with patch("sys.stderr", StringIO()):
                render_prompt.parse_args([])

    def test_invalid_arg_format_exits(self) -> None:
        with self.assertRaises(SystemExit):
            with patch("sys.stderr", StringIO()):
                render_prompt.parse_args(["create-story", "bad-arg"])

    def test_hyphen_to_underscore_conversion(self) -> None:
        _, variables = render_prompt.parse_args(
            ["create-story", "--project-root=/r", "--epic-num=1"]
        )
        self.assertEqual(variables["project_root"], "/r")
        self.assertEqual(variables["epic_num"], "1")

    def test_spec_file_not_found_exits(self) -> None:
        with self.assertRaises(SystemExit):
            with patch("sys.stderr", StringIO()):
                render_prompt.parse_args(
                    ["code-review", "--spec-file=/nonexistent/file.md"]
                )

    def test_diff_file_not_found_exits(self) -> None:
        with self.assertRaises(SystemExit):
            with patch("sys.stderr", StringIO()):
                render_prompt.parse_args(
                    ["code-review", "--diff-file=/nonexistent/file.diff"]
                )


class TestMainIntegration(unittest.TestCase):
    """Integration test using main() entry point."""

    def test_main_outputs_to_stdout(self) -> None:
        stdout = StringIO()
        with patch("sys.stdout", stdout):
            render_prompt.main(
                [
                    "create-story",
                    "--project_root=/proj",
                    "--epic_num=2",
                    "--story_num=1",
                    "--title=Hello World",
                ]
            )
        output = stdout.getvalue()
        self.assertIn("story 2.1 in epic 2", output)
        self.assertIn('"Hello World"', output)


if __name__ == "__main__":
    unittest.main()
