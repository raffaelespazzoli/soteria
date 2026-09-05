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
- `--review-model=<model-slug>` — LLM model for adversarial code review (must differ from dev model to prevent self-review blind spots; default: auto-select a different model)
- `--headless` — full automation mode for CI/pipelines: auto-approves Phase A specs (with warning log), fails fast on decision-needed relays, auto-cleans orphaned worktrees, and writes a structured JSON summary on completion/halt. Implies `--commit-policy=auto --skip-confirmations`

**Core contract:**
- Every sub-skill runs in a **fresh subagent** (clean LLM context)
- Sub-skill subagents run autonomously but **relay "decision needed" questions** back to you for the user
- On any failure: **halt with resume instructions** — re-invoking with the same epic number picks up where it left off
- `sprint-status.yaml` is the checkpoint — story statuses determine what's been done and what remains
- Interactive by default: report progress after each story, soft gates between phases
- Phase A (story specs) parallelizes by dependency layer via concurrent subagents
- Phase B (implementation) parallelizes by dependency layer via **git worktrees** — each story gets its own branch and working directory, merged back after review

## On Activation

Load available config from `{project-root}/_bmad/config.yaml` and `{project-root}/_bmad/config.user.yaml` (root level and `bmm` section). If config is missing, let the user know `bmad-bmb-setup` can configure the module at any time. Use sensible defaults for anything not configured.

Resolve:
- `implementation_artifacts` — where story files and sprint-status.yaml live
- `planning_artifacts` — where epic source files live
- `communication_language` — for user-facing messages
- `user_name` — for addressing the user in progress reports

**Intent guard:** Before proceeding, confirm the user intends full epic orchestration. If the input looks like a single story reference (e.g., "13.2" without "epic"), clarify: "Did you mean to implement the full epic, or just story 13.2? For a single story, use `bmad-dev-story` instead."

Then read fully and follow `./references/phase-logic.md`.
