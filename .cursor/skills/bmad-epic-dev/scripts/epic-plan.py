#!/usr/bin/env python3
"""Deterministic pre-pass for bmad-epic-dev.

Parses sprint-status.yaml and the epic source file to produce a
structured JSON execution plan: dependency graph, topological layers,
story states, and parallelism opportunities.

Usage:
    python3 epic-plan.py <epic-number> [--impl-dir <path>] [--plan-dir <path>]

Output (stdout): JSON execution plan.
"""

# /// script
# requires-python = ">=3.9"
# ///

from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
from collections import defaultdict, deque
from pathlib import Path


_ACTION_MAP: dict[str, tuple[str, bool]] = {
    "backlog": ("create-story", False),
    "ready-for-dev": ("dev-story", False),
    "in-progress": ("dev-story", False),
    "review": ("code-review", False),
    "merge-conflict": ("merge", False),
    "done": ("skip", False),
}


def required_action_for(status: str) -> tuple[str, bool]:
    """Return ``(action, warned)`` for a story status."""
    return _ACTION_MAP.get(status, ("create-story", True))


def find_sprint_status(impl_dir: Path) -> Path | None:
    p = impl_dir / "sprint-status.yaml"
    return p if p.exists() else None


def parse_sprint_status(path: Path, epic_num: str) -> dict:
    """Extract epic status and story statuses from sprint-status.yaml."""
    text = path.read_text(encoding="utf-8")

    epic_status = None
    stories: dict[str, str] = {}

    in_dev_status = False
    epic_key = f"epic-{epic_num}"
    story_prefix = f"{epic_num}-".replace(".", "-")

    for line in text.splitlines():
        stripped = line.strip()

        if stripped.startswith("development_status:"):
            in_dev_status = True
            continue

        if not in_dev_status:
            continue

        if not stripped or stripped.startswith("#"):
            continue

        match = re.match(r"^(\S[\w.-]+)\s*:\s*(.+)$", stripped)
        if not match:
            continue

        key, value = match.group(1), match.group(2).strip()

        if key == epic_key:
            epic_status = value
        elif key.startswith(story_prefix) or key.startswith(epic_num.replace(".", "-") + "-"):
            stories[key] = value

    return {"epic_status": epic_status, "stories": stories}


def find_epic_file(epic_num: str, impl_dir: Path, plan_dir: Path | None) -> Path | None:
    """Locate the epic source file with dependency table."""
    candidates = sorted(impl_dir.glob(f"{epic_num}-epic-*.md"))
    if candidates:
        return candidates[0]

    candidates = sorted(impl_dir.glob(f"{epic_num.replace('.', '-')}-epic-*.md"))
    if candidates:
        return candidates[0]

    if plan_dir and plan_dir.exists():
        for f in plan_dir.glob("*epic*.md"):
            text = f.read_text(encoding="utf-8")
            if f"Epic {epic_num}" in text or f"# Epic {epic_num}:" in text:
                return f

    return None


def parse_stories_table(path: Path, epic_num: str) -> list[dict]:
    """Parse the markdown Stories table for dependency information."""
    text = path.read_text(encoding="utf-8")
    stories = []

    table_pattern = re.compile(
        r"\|\s*(\d+(?:\.\d+)?[a-z]?)\s*\|\s*([^|]+)\s*\|\s*([^|]+)\s*\|\s*([^|]+)\s*\|"
    )

    in_stories_section = False
    for line in text.splitlines():
        if re.match(r"^##\s+Stories", line, re.IGNORECASE):
            in_stories_section = True
            continue

        if in_stories_section and line.startswith("## "):
            break

        if not in_stories_section:
            continue

        if "|---" in line or "| Story" in line or "| ---" in line:
            continue

        m = table_pattern.match(line.strip())
        if m:
            story_num = m.group(1).strip()
            title = m.group(2).strip()
            dep_raw = m.group(3).strip()

            deps = []
            if dep_raw.lower() not in ("none", "—", "-", ""):
                deps = [d.strip() for d in re.split(r"[,;]", dep_raw) if d.strip()]

            stories.append({
                "story_num": story_num,
                "title": title,
                "dependencies": deps,
            })

    return stories


def build_dependency_layers(stories: list[dict]) -> tuple[list[list[str]], list[str]]:
    """Topological sort into dependency layers (Kahn's algorithm).

    Returns ``(layers, circular)`` where *circular* contains any story
    numbers that participate in a dependency cycle.
    """
    story_nums = {s["story_num"] for s in stories}
    story_map = {s["story_num"]: s for s in stories}

    adj: dict[str, list[str]] = defaultdict(list)
    in_degree: dict[str, int] = {s: 0 for s in story_nums}

    for s in stories:
        for dep in s["dependencies"]:
            if dep in story_nums:
                adj[dep].append(s["story_num"])
                in_degree[s["story_num"]] += 1

    layers: list[list[str]] = []
    queue = deque(s for s in story_nums if in_degree[s] == 0)

    while queue:
        layer = sorted(queue)
        layers.append(layer)
        next_queue: deque[str] = deque()
        for node in layer:
            for neighbor in adj[node]:
                in_degree[neighbor] -= 1
                if in_degree[neighbor] == 0:
                    next_queue.append(neighbor)
        queue = next_queue

    placed = {s for layer in layers for s in layer}
    circular = sorted(story_nums - placed)

    return layers, circular


def story_key_from_num(epic_num: str, story_num: str, stories_status: dict[str, str]) -> str | None:
    """Find the sprint-status key matching a story number."""
    normalized = story_num.replace(".", "-")
    for key in stories_status:
        if key.startswith(normalized + "-") or key == normalized:
            return key
    return None


def check_worktrees(epic_num: str) -> dict:
    """Inspect git worktrees for branches matching ``epic-{N}/story-*``.

    Returns ``{"orphaned": [...], "active": [...], "error": None}``
    or an error payload when git is unavailable.
    """
    try:
        proc = subprocess.run(
            ["git", "worktree", "list", "--porcelain"],
            capture_output=True,
            text=True,
            timeout=10,
        )
    except FileNotFoundError:
        return {"orphaned": [], "active": [], "error": "git not found"}
    except Exception as exc:
        return {"orphaned": [], "active": [], "error": str(exc)}

    if proc.returncode != 0:
        return {"orphaned": [], "active": [], "error": f"git exited {proc.returncode}: {proc.stderr.strip()}"}

    prefix = f"epic-{epic_num}/story-"
    active: list[str] = []
    orphaned: list[str] = []

    current_wt: str | None = None
    current_branch: str | None = None
    is_bare = False

    for line in proc.stdout.splitlines():
        if line.startswith("worktree "):
            if current_branch and current_branch.startswith(prefix):
                (orphaned if is_bare else active).append(current_branch)
            current_wt = line.split(" ", 1)[1]
            current_branch = None
            is_bare = False
        elif line.startswith("branch "):
            ref = line.split(" ", 1)[1]
            current_branch = ref.split("/")[-1] if "/" in ref else ref
            # Reconstruct full short branch: refs/heads/epic-N/story-X -> epic-N/story-X
            parts = ref.replace("refs/heads/", "")
            current_branch = parts
        elif line.strip() == "bare":
            is_bare = True

    # flush last entry
    if current_branch and current_branch.startswith(prefix):
        (orphaned if is_bare else active).append(current_branch)

    return {"orphaned": orphaned, "active": active, "error": None}


def main() -> None:
    parser = argparse.ArgumentParser(description="Epic execution plan generator")
    parser.add_argument("epic_num", help="Epic number (e.g., 13)")
    parser.add_argument("--impl-dir", default=None, help="Implementation artifacts directory")
    parser.add_argument("--plan-dir", default=None, help="Planning artifacts directory")
    parser.add_argument("-o", "--output", default=None, help="Output file (default: stdout)")
    parser.add_argument("--check-worktrees", action="store_true", help="Inspect git worktrees for epic branches")
    args = parser.parse_args()

    epic_num = args.epic_num

    project_root = Path.cwd()
    impl_dir = Path(args.impl_dir) if args.impl_dir else project_root / "_bmad-output" / "implementation-artifacts"
    plan_dir = Path(args.plan_dir) if args.plan_dir else project_root / "_bmad-output" / "planning-artifacts"

    result: dict = {
        "epic_num": epic_num,
        "status": "ok",
        "errors": [],
    }

    ss_path = find_sprint_status(impl_dir)
    if not ss_path:
        result["status"] = "error"
        result["errors"].append(f"sprint-status.yaml not found in {impl_dir}")
        _output(result, args.output)
        return

    ss_data = parse_sprint_status(ss_path, epic_num)
    result["epic_status"] = ss_data["epic_status"]
    result["story_statuses"] = ss_data["stories"]

    if ss_data["epic_status"] == "done":
        result["status"] = "done"
        result["errors"].append(f"Epic {epic_num} is already done")
        _output(result, args.output)
        return

    epic_file = find_epic_file(epic_num, impl_dir, plan_dir)
    if not epic_file:
        result["status"] = "error"
        result["errors"].append(f"No epic file found for epic {epic_num}")
        _output(result, args.output)
        return

    result["epic_file"] = str(epic_file)

    stories = parse_stories_table(epic_file, epic_num)
    if not stories:
        result["status"] = "error"
        result["errors"].append(f"No stories table found in {epic_file}")
        _output(result, args.output)
        return

    for s in stories:
        key = story_key_from_num(epic_num, s["story_num"], ss_data["stories"])
        s["sprint_key"] = key
        s["status"] = ss_data["stories"].get(key, "unknown") if key else "unknown"
        action, warned = required_action_for(s["status"])
        s["required_action"] = action
        if warned:
            s["required_action_warned"] = True

    layers, circular = build_dependency_layers(stories)

    result["stories"] = stories
    result["dependency_layers"] = layers
    if circular:
        result["circular_dependencies"] = circular

    if args.check_worktrees:
        result["worktrees"] = check_worktrees(epic_num)

    result["summary"] = {
        "total_stories": len(stories),
        "layers": len(layers),
        "backlog": sum(1 for s in stories if s["status"] == "backlog"),
        "ready_for_dev": sum(1 for s in stories if s["status"] == "ready-for-dev"),
        "in_progress": sum(1 for s in stories if s["status"] in ("in-progress", "review")),
        "done": sum(1 for s in stories if s["status"] == "done"),
        "phase_a_needed": any(s["status"] == "backlog" for s in stories),
        "phase_b_needed": any(s["status"] in ("ready-for-dev", "in-progress", "review") for s in stories),
    }

    _output(result, args.output)
    sys.exit(0 if result["status"] in ("ok", "done") else 1)


def _output(data: dict, output_path: str | None) -> None:
    text = json.dumps(data, indent=2) + "\n"
    if output_path:
        Path(output_path).write_text(text, encoding="utf-8")
        print(f"Plan written to {output_path}", file=sys.stderr)
    else:
        print(text)


if __name__ == "__main__":
    main()
