#!/usr/bin/env python3
# /// script
# requires-python = ">=3.9"
# dependencies = []
# ///
"""Render subagent prompt templates with variable substitution."""

from __future__ import annotations

import sys
import re
from pathlib import Path
from typing import Dict, List, Optional, Set, Tuple

# ---------------------------------------------------------------------------
# Prompt templates
# ---------------------------------------------------------------------------

TEMPLATES: Dict[str, str] = {
    "create-story": """\
IMPORTANT — Team customizations: Before starting, check for and load:
  {project_root}/_bmad/custom/bmad-create-story.toml
If the file exists, read it fully and apply ALL overrides (persistent_facts,
activation_steps_append, on_complete, etc.) before proceeding with the skill.

Run the bmad-create-story skill for story {epic_num}.{story_num} in epic {epic_num}.
The story title is "{title}".
Process the story fully and autonomously — no user interaction needed.
If you encounter a blocking issue that requires a decision, describe the
decision needed clearly and halt.
When complete, report: story_key, output_file_path, status (success/failed/decision_needed),
and a one-line summary.""",

    "dev-story": """\
IMPORTANT — Team customizations: Before starting, check for and load:
  {project_root}/_bmad/custom/bmad-dev-story.toml
If the file exists, read it fully and apply ALL overrides (activation_steps_append,
on_complete, etc.) before proceeding with the skill.

You are implementing story {epic_num}.{story_num} for epic {epic_num}.
The story file is at: {implementation_artifacts}/{story_key}.md

Run the bmad-dev-story skill to completion.
Implement all tasks, run all tests, mark complete.
If you encounter a blocking issue requiring a human decision, describe it
clearly and halt. Do NOT make assumptions on behalf of the user.

⚠️ DO NOT run code review — that is handled separately by the orchestrator
using a different LLM for adversarial review.

When complete, commit your changes:
git add -A && git commit -m "feat(epic-{epic_num}): {story_key} — {title}"

Report: story_key, status (success/failed/decision_needed),
files_changed_count, commit_sha, and a one-line summary.""",

    "code-review": """\
IMPORTANT — Team customizations: Before starting, check for and load:
  {project_root}/_bmad/custom/bmad-code-review.toml
If the file exists, read it fully and apply all overrides before proceeding.

Run the bmad-code-review skill on the following changes for story {story_key}.

Story specification:
<spec>
{story_spec_content}
</spec>

Code diff to review:
<diff>
{diff_output}
</diff>

Run the review fully and autonomously — complete all layers (Blind Hunter,
Edge Case Hunter, Acceptance Auditor), produce the triage report.
If the review raises a design/requirements question (not a code fix),
describe it and halt.

When complete, report: story_key, verdict (APPROVED/CHANGES_REQUESTED),
must_fix_count, should_fix_count, findings_summary (bullet list of key items),
and a one-line summary.""",

    "fix-pass": """\
IMPORTANT — Team customizations: Before starting, check for and load:
  {project_root}/_bmad/custom/bmad-dev-story.toml
If the file exists, read it fully and apply all overrides before proceeding.

You are fixing review findings for story {story_key} in epic {epic_num}.
The story branch is: epic-{epic_num}/story-{story_key}

Adversarial review findings to address:
<findings>
{review_findings}
</findings>

Fix all must-fix items. Address should-fix items where the fix is straightforward.
Skip consider items unless trivial.

Run the full test suite after fixes to verify no regressions.

Commit fixes:
git add -A && git commit -m "fix(epic-{epic_num}): {story_key} — address review findings"

When complete, report: files_changed, fixes_applied, tests_pass (yes/no),
and a one-line summary of what was fixed.""",
}

# Required keys per template
REQUIRED_KEYS: Dict[str, List[str]] = {
    "create-story": ["project_root", "epic_num", "story_num", "title"],
    "dev-story": [
        "project_root",
        "epic_num",
        "story_num",
        "story_key",
        "title",
        "implementation_artifacts",
    ],
    "code-review": [
        "project_root",
        "story_key",
        "story_spec_content",
        "diff_output",
    ],
    "fix-pass": [
        "project_root",
        "epic_num",
        "story_key",
        "review_findings",
    ],
}

# Regex to find all {key} placeholders (excluding {{ escaped braces }})
_VAR_RE = re.compile(r"\{([a-zA-Z_][a-zA-Z0-9_]*)\}")


def _find_template_vars(template: str) -> Set[str]:
    """Return the set of variable names found in a template."""
    return set(_VAR_RE.findall(template))


def render(template_name: str, variables: Dict[str, str]) -> str:
    """Render a named template with the given variables.

    Unknown keys in the template are left as-is.
    Missing required keys produce a warning to stderr.
    """
    if template_name not in TEMPLATES:
        print(
            f"error: unknown template '{template_name}'. "
            f"Available: {', '.join(sorted(TEMPLATES))}",
            file=sys.stderr,
        )
        sys.exit(1)

    template = TEMPLATES[template_name]
    required = REQUIRED_KEYS.get(template_name, [])

    for key in required:
        if key not in variables:
            print(
                f"warning: required key '{key}' not provided for "
                f"template '{template_name}'",
                file=sys.stderr,
            )

    def _replace(match: re.Match) -> str:  # type: ignore[type-arg]
        key = match.group(1)
        if key in variables:
            return variables[key]
        return match.group(0)  # leave as-is

    return _VAR_RE.sub(_replace, template)


def parse_args(argv: List[str]) -> Tuple[str, Dict[str, str]]:
    """Parse CLI arguments into (template_name, variables).

    Supports:
      --key=value           → variables[key] = value
      --spec-file=PATH      → variables[story_spec_content] = read(PATH)
      --diff-file=PATH      → variables[diff_output] = read(PATH)
    """
    if not argv:
        print("usage: render-prompt.py <template-name> [--key=value ...]", file=sys.stderr)
        sys.exit(1)

    template_name = argv[0]
    variables: Dict[str, str] = {}

    for arg in argv[1:]:
        if not arg.startswith("--") or "=" not in arg:
            print(f"error: invalid argument '{arg}'. Use --key=value format.", file=sys.stderr)
            sys.exit(1)

        key, _, value = arg[2:].partition("=")

        if key == "spec-file":
            path = Path(value)
            if not path.is_file():
                print(f"error: spec-file '{value}' not found", file=sys.stderr)
                sys.exit(1)
            variables["story_spec_content"] = path.read_text(encoding="utf-8")
        elif key == "diff-file":
            path = Path(value)
            if not path.is_file():
                print(f"error: diff-file '{value}' not found", file=sys.stderr)
                sys.exit(1)
            variables["diff_output"] = path.read_text(encoding="utf-8")
        else:
            variables[key.replace("-", "_")] = value

    return template_name, variables


def main(argv: Optional[List[str]] = None) -> None:
    """Entry point."""
    if argv is None:
        argv = sys.argv[1:]
    template_name, variables = parse_args(argv)
    result = render(template_name, variables)
    print(result)


if __name__ == "__main__":
    main()
