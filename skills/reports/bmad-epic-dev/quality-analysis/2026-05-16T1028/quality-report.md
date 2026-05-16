# BMad Method · Quality Analysis: bmad-epic-dev

**Analyzed:** 2026-05-16T10:28Z | **Path:** `/home/rspazzol/git/dr-orchestrator/.claude/skills/bmad-epic-dev`
**Interactive report:** quality-report.html

## Assessment

**Good** — The skill is a coherent, delegation-first epic orchestrator with a lean activation surface, clear phased goals, and strong operational patterns (DAG-layer parallelism in Phase A, sequential Phase C with rationale, resume via `sprint-status.yaml`). The main drag is **deterministic work still routed through the orchestrator LLM** (especially Step 0), which couples **high token variance** with **tooling that misclassifies** the skill as a simple utility. Secondary gaps cluster around **reference durability under context compaction**, **BMad convention drift** (single `references/` phase doc vs numbered stages), and **policy friction** (automated commits and confirmation gates vs common user rules and headless use).

## What's Broken

### Automated workflow type contradicts actual orchestration (`workflow-integrity`)

**Where:** `workflow-integrity-prepass.json` reports `workflow_type: "simple-utility"` while `SKILL.md` + `references/phase-logic.md` implement multi-phase orchestration (validation, DAG planning, Phase A/C, completion, resume).

**Fix:** Add an explicit workflow-type signal in `SKILL.md` (e.g. a short section or frontmatter-adjacent convention) and/or extend the prepass classifier so “primary logic in `references/*.md` with named phases” is not labeled `simple-utility`.

## Opportunities

### 1. Shift deterministic orchestration to pre-pass tooling (high — 9 observations)

Heavy YAML merges, filesystem discovery, markdown table parsing, DAG construction, layer grouping, and sprint-status cross-checks are **mechanical** but currently sit on the orchestrator model (Step 0 and several gates). That inflates tokens (script scanner estimates **~1.3k–4.5k** tokens per full epic shiftable off-LLM), latency, and failure modes (ambiguous epic match, parse errors). The same structural gap feeds **epic discovery ambiguity** and leaves workflow engines without a machine-readable graph.

**Fix:** Introduce a small CLI or PEP 723 Python pre-pass (e.g. unified `bmad-context` / epic-plan entrypoint with `--help`) that emits compact JSON (`epic_plan.json`): resolved config, epic resolution, story DAG, layers, resume hints; optionally `--verify` post-conditions for Phase A/C. Point prompts at narrating that payload instead of re-deriving structure every run.

**Observations:**

- Step 0 epic resolution, sprint-status validation, story-table parse, DAG, topological layers, merge — `references/phase-logic.md` ~3–32 — script-opportunities (high)
- Merge and validate `_bmad/config.yaml` + `config.user.yaml` — `SKILL.md` 23–30 — script-opportunities (medium)
- Post–Phase A batch verification of `sprint-status.yaml` — `references/phase-logic.md` 61–63 — script-opportunities (medium)
- Verify story status after dev-story (expect `review`) — `references/phase-logic.md` 114 — script-opportunities (medium)
- Detect new backlog stories after Phase C — `references/phase-logic.md` 159–165 — script-opportunities (medium)
- Completion aggregates (counts, epic `done`) — `references/phase-logic.md` 171–177 — script-opportunities (medium)
- Resume mapping — `references/phase-logic.md` 184–196 — script-opportunities (low; bundle with tool)
- Optional “extract only” path when epic markdown is huge — `references/phase-logic.md` 11–18 — execution-efficiency (medium)
- Epic source file discovery / ambiguous matches — `references/phase-logic.md` (Step 0 patterns) — skill-cohesion (medium)

### 2. Harden `phase-logic.md` and metadata against compaction & convention drift (medium — 7 observations)

`references/phase-logic.md` is the **executable truth** but lacks a short anchor mirroring the non‑negotiable contract from `SKILL.md`, lacks BMad-style language/output headers, and diverges from “numbered root stage files” expectations—so automated integrity checks see **zero stages** while behavior is clearly phased. The description hook is slightly long for conservative triggering, and Phase **A → C** skips **B** without explanation.

**Fix:** Add a 3–6 line **anchor block** (fresh subagents, relay decisions, halt/resume, `sprint-status.yaml` as source of truth) plus a minimal config/language preamble; tighten description first clause; add one line on Phase B (reserved or rename phases); strengthen “read fully” wording to **authoritative sequential execution**; either split phase logic into root numbered stages **or** document this references-only pattern as a supported variant for scanners.

**Observations:**

- Self-containment under context compaction — `references/phase-logic.md` 1–35 — prompt-craft (medium)
- Missing BMad-style config header on executable prompts — `SKILL.md` 1–4; `references/phase-logic.md` — prompt-craft (medium)
- Complex-workflow conventions without root stage files — `SKILL.md`; `references/phase-logic.md` — workflow-integrity (medium)
- No language / output-language header in phase reference — `references/phase-logic.md` 1 — workflow-integrity (medium)
- Description summary length — `SKILL.md` 3 — workflow-integrity (medium)
- Reference loading phrasing could be stronger — `SKILL.md` 32 — prompt-craft (low)
- Phase letter gap (A then C) — `references/phase-logic.md` 36, 78 — workflow-integrity / prompt-craft / skill-cohesion (low)

### 3. Align commits, intent routing, subagent contracts, and headless modes (medium — 8 observations)

Automated `git commit` steps **conflict** with common workspace rules (“commit only when asked”). The workflow jumps into epic validation **without intent confirmation**, risking expensive misfires. Subagent prompts lack a **structured completion payload**, inviting verbose handoffs. Experts hit **soft gates** even when only Phase C remains; automators lack **dry-run / no-commit / structured exit** contracts.

**Fix:** Add a **one-line intent gate** (full epic vs single-story vs review-only) with pointers to sibling skills; introduce **`commit_policy`** (default / defer / confirm-each) reconciled with orchestration checkpoints; define **optional JSON “phase complete” schema** for create-story / dev-story / code-review returns; document **`skip_confirmations` / resume-silent** behaviors for advanced or headless runs plus a minimal JSON exit summary.

**Observations:**

- Commit instructions vs user-driven commit norms — Phase A step 7; Phase C — skill-cohesion (medium)
- Intent-before-ingestion / mis-invocation risk — workflow entry — enhancement-opportunities (high-opportunity)
- Commit policy configurability — Phase A/C — enhancement-opportunities (high-opportunity)
- Expert fast-path / resume-silent — Phase boundaries — enhancement-opportunities (high-opportunity)
- Subagent prompts prose-only handoffs — `references/phase-logic.md` 47–54, 88–96, 118–127 — execution-efficiency (medium)
- Parallelism expectation vs sequential Phase C — `SKILL.md` overview vs reference — enhancement-opportunities (medium)
- Headless parameters (`--yes`, `commit_policy`, structured exit) — enhancement-opportunities (medium)
- Create-story “no user interaction” vs orchestrator “relay decisions” — subagent wording — skill-cohesion (low)

## Strengths

- **Lean activation:** `SKILL.md` ~33 lines (~458 tokens per prepass); progressive disclosure to `references/phase-logic.md` — prompt-craft / workflow-integrity.
- **Clear orchestrator identity:** Implement via delegation; specialized sub-skills for create/dev/review — prompt-craft / cohesion.
- **Concrete subagent templates:** Fenced prompts for `bmad-create-story`, `bmad-dev-story`, `bmad-code-review` — prompt-craft.
- **Efficient structure:** Phase A parallel by DAG layer; Phase C sequential with explicit rationale; no circular dependency issues in instructions — execution-efficiency / cohesion.
- **Operational resilience:** Resume table, halt-with-resume, sprint-status as checkpoint, spawned-story loop — cohesion / efficiency.
- **Path hygiene:** Path standards scan **pass**, zero findings — path-standards-temp.json.

## Detailed Analysis

### Structure & Integrity

Structurally sound for daily use: frontmatter naming, `## Overview` / `## On Activation`, correct `./references/phase-logic.md` wiring, no forbidden exit sections, no orphaned placeholders. Gaps are **classification vs content**, **stage-file convention mismatch**, and **metadata/language headers** — largely covered by themes above. Pre-pass reported zero structural issues; manual review overrides workflow typing.

### Craft & Writing Quality

Overview quality is **appropriate** and rationale appears where procedures could be questioned (e.g. sequential Phase C). Progressive disclosure is **good** (long procedure externalized). Prompt health metrics reflect layout (zero root stage `.md` files); **`phase-logic.md` should be treated as the scored stage prompt**. Remaining craft items: anchor block, config binding, slightly stronger load instruction — see Theme 2.

### Cohesion & Design

**Stage flow:** strong — validate → plan → Phase A → Phase C → completion with explicit review spawn loop. **Purpose alignment:** moderate — soft gates exist but automated commits clash with typical agent rules. **Complexity:** strong for scope. **Completeness:** moderate — epic discovery edge cases, `git add` scope differs Phase A vs C (Phase A broad vs `git add -A` in C.3). **Redundancy:** strong. **Dependency graph at skill level:** moderate/N/A — prompt-only, no YAML manifest. **External integration:** strong — clear trio of sub-skills.

### Execution Efficiency

Delegation-first, sensible parallelism, fail-fast Step 0 before expensive phases. Main efficiency lever: **structured subagent returns** (medium). Optional future split if `phase-logic.md` grows. Pre-pass dependency graph empty by design (markdown-orchestrated).

### User Experience

Journeys analyzed: first-timer (opaque multi-epic backlog, commit discomfort), expert (confirmation fatigue, no implement-only fast path), confused (wrong skill expensive), edge-case (ambiguous epic file, spawned stories surprise), hostile env (invalid YAML, git hook failures unclear), automator (gates block CI). **Headless:** partially adaptable — externalized state helps; confirmations and relays block full automation without parameters.

### Script Opportunities

No `scripts/` directory (`scripts-temp.json`: informational). Intelligence skewed to LLM for deterministic steps; highest ROI is **Step 0 bundle + config merge** as one tool, then optional validators. Aggregate token savings estimate **~1,300–4,500** tokens per full epic if mechanical steps exit the model.

## Recommendations

1. **Ship epic-plan / bmad-context pre-pass JSON + tighten classifier** — resolves the largest cluster (Step 0, config, many gates, epic ambiguity, tooling misclassification). **Effort:** medium–high.
2. **Anchor block + language/config preamble + workflow-type signal + description hook + Phase B note** — improves compaction survival, scanner alignment, and triggering. **Effort:** low.
3. **Intent gate + `commit_policy` + optional confirmation bypass for resume-heavy runs** — reduces misfires and policy conflict. **Effort:** medium.
4. **Standardized subagent completion schema (JSON or capped bullets)** — reduces handoff tokens and clarification rounds. **Effort:** medium.
5. **Document parallelism scope in Overview** (Phase A parallel; Phase C sequential) — quick clarity for experts. **Effort:** low.
6. **Align `git add` policy** across Phase A and C with one sentence of rationale. **Effort:** low.
