---
name: bmad-epic-dev
description: "Orchestrate full epic development end-to-end. Use when the user says 'dev this epic' or 'implement epic [number]'."
---

# Epic Dev

## Overview

This skill drives an entire epic from backlog to done — creating story specs, implementing each story, and running adversarial code review — all with fresh LLM context per step. It parses the epic's dependency tree to optimize execution order and parallelize where safe.

Act as a disciplined build orchestrator. You don't write code or specs yourself — you delegate to specialized skills (`bmad-create-story`, `bmad-dev-story`, `bmad-code-review`) via subagents, track progress, and relay decisions back to the user.

**Args:**
- Epic number (e.g., `13`) — required
- `--commit-policy=<auto|ask|skip>` — controls git commit behavior (default: `auto`)
  - `auto`: commit automatically at prescribed points
  - `ask`: prompt user before each commit
  - `skip`: never commit (user handles commits manually)
- `--skip-confirmations` — suppress soft-gate pauses between phases (for experienced users resuming a known-good epic)

**Model routing:**
- Story creation and development (`bmad-create-story`, `bmad-dev-story`): **claude-opus-4-6** with extended/high thinking
- Code review (`bmad-code-review`): **gpt-5.4** with medium thinking
- Apply via the platform's subagent model parameter (Cursor: `model` field on Task tool; Claude Code: `--model` flag). If the preferred model isn't available on the current platform, use the best available alternative and note the substitution.

**Core contract:**
- Every sub-skill runs in a **fresh subagent** (clean LLM context)
- Sub-skill subagents run autonomously but **relay "decision needed" questions** back to you for the user
- On any failure: **halt with resume instructions** — re-invoking with the same epic number picks up where it left off
- `sprint-status.yaml` is the checkpoint — story statuses determine what's been done and what remains
- Interactive by default: report progress after each story, soft gates between phases
- Phase A ends with a **mandatory review gate** — all story specs are listed and the user must explicitly confirm before development begins
- Phase B code review runs **non-interactively** unless a genuine design/requirements decision is needed. When interrupting for a decision, provide rich context: describe the issue thoroughly, give a concrete example, and lay out the available choices with trade-offs before waiting for user input
- Phase A (story specs) parallelizes by dependency layer via concurrent subagents
- Phase B (implementation) parallelizes by dependency layer via **git worktrees** — each story gets its own branch and working directory, merged back after review

## On Activation

Load available config from `{project-root}/_bmad/config.yaml` and `{project-root}/_bmad/config.user.yaml` (root level and `bmm` section). If config is missing, let the user know `bmad-bmb-setup` can configure the module at any time. Use sensible defaults for anything not configured.

Resolve:
- `implementation_artifacts` — where story files and sprint-status.yaml live
- `planning_artifacts` — where epic source files live
- `user_name`, `communication_language`

**Intent guard:** Before proceeding, confirm the user intends full epic orchestration. If the input looks like a single story reference (e.g., "13.2" without "epic"), clarify: "Did you mean to implement the full epic, or just story 13.2? For a single story, use `bmad-dev-story` instead."

Then read fully and follow `./references/phase-logic.md`.
