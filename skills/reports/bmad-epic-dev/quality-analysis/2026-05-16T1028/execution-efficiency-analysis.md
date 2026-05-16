# Execution Efficiency Analysis: `bmad-epic-dev`

**Scanner:** Execution Efficiency (unified parallelization, subagent delegation, context, stage ordering, dependencies)  
**Skill path:** `.claude/skills/bmad-epic-dev`  
**Artifacts reviewed:** `SKILL.md`, `references/phase-logic.md`  
**Pre-pass:** `execution-deps-prepass.json` (v1.0.0) — workflow engine stages empty; no automated graph issues.

## Assessment

`bmad-epic-dev` is **structurally efficient** for its role: it delegates heavy work to sub-skills, **parallelizes story-spec creation by dependency layer** where the runtime allows, and **correctly serializes implementation/review** on a single branch with an explicit rationale. The main gaps are **subagent return-format discipline** (risk of verbose handoffs) and **small activation-time optimizations** (batched config reads). No circular dependencies or subagent-from-subagent anti-patterns appear in this skill’s instructions.

## Key findings

| Severity | Location | Current pattern | Efficient alternative | Est. savings |
| -------- | -------- | ----------------- | --------------------- | ------------ |
| **Medium** | `references/phase-logic.md:47-54`, `:88-96`, `:118-127` | Subagent prompts are prose-only (“process fully,” “halt if …”) with no required structured completion payload | Add “ONLY return” JSON (e.g., `{story, phase, status, decision_needed?, summary_max_tokens}`) or a fixed bullet template capped at N lines | **High variance:** tens to hundreds of tokens per handoff; fewer clarification rounds |
| **Low** | `SKILL.md:25-26` | On activation, load `_bmad/config.yaml` then `_bmad/config.user.yaml` as separate sequential reads | Batch both reads in one assistant turn (parallel tool calls) | Marginal latency; **~0** tokens |
| **Low** | `SKILL.md:32` | Parent always reads `references/phase-logic.md` in full on activation | Acceptable at current size (~200 lines). If it grows, split “execution checklist” vs “deep reference” and load the latter only when debugging | **0 today**; avoids future **500–2k+** token tax if file grows |
| **Low** | `references/phase-logic.md:11-18` | Parent discovers and parses the epic markdown to build the DAG | For very large epic docs, delegate “extract Stories table → JSON graph” to a subagent so the parent never ingests full narrative body | **0–large** depending on epic file size (optional threshold: 5+ stories or file clearly huge) |

**Pre-pass alignment:** `execution-deps-prepass.json` reports `status: "pass"`, `cycles: []`, `issues: []`. The empty `stages` / `dependency_graph` reflect that this skill is **markdown-orchestrated**, not a workflow-engine YAML graph — there is nothing to optimize in `after` / `before` metadata within this repo artifact.

## Optimization opportunities

1. **Standardize subagent handoffs (medium impact):** Define one JSON schema for “phase complete / blocked” used by create-story, dev-story, and code-review delegation prompts. Enables the parent to parse state without re-reading logs or story files unless necessary.

2. **Optional “extract only” subagent for Step 0 (conditional impact):** If epic markdown grows (embedded diagrams, long PRDs), add an explicit branch: “If epic source exceeds X tokens or Y KB, spawn extraction subagent; else parse inline.” Reduces parent context when epics are monolithic.

3. **Phase C remains sequential by design (no change recommended):** The doc already defers parallel implementation to a hypothetical worktree model (`references/phase-logic.md:200-202`). Keeping v1 sequential avoids merge contention — **preserving this is efficiency-positive** for human merge/review cost, not just tokens.

## What’s already efficient

- **Layer-based parallelism in Phase A** (`references/phase-logic.md:40-41`, `198-201`): Matches the scanner’s guidance for independent batch work via parallel subagents; centralized `sprint-status.yaml` updates after each batch avoid write races.

- **Fail-fast and ordering:** Step 0 validates epic state and builds the plan before expensive Phase A/C work (`references/phase-logic.md:3-34`). Phase A completes before Phase C; new backlog stories from review loop back explicitly (`references/phase-logic.md:157-165`).

- **Resume model:** Checkpointing via `sprint-status.yaml` avoids redoing completed substeps — good token/time amortization on partial runs (`references/phase-logic.md:184-196`, `SKILL.md:19-20`).

- **Delegation-first contract:** Parent acts as orchestrator, not implementer (`SKILL.md:12`, `16-17`), aligning with read-avoidance goals for code and story authoring.

- **No subagent nesting requirement in this skill:** Parallelism is expressed as sibling subagents coordinated by the parent; Phase C chains dev → review in the parent — consistent with “subagents cannot spawn subagents” constraints.

---

**File written:** `execution-efficiency-analysis.md`
