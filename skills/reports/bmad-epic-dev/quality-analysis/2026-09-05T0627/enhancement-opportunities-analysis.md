# Enhancement Opportunities Analysis: `bmad-epic-dev`

**Scanner:** BMad Workflow Builder — Creative Edge-Case & Experience Innovation (`quality-scan-enhancement-opportunities.md`)  
**Skill path:** `.cursor/skills/bmad-epic-dev`  
**Artifacts reviewed:** `SKILL.md`, `references/phase-logic.md`, `scripts/epic-plan.py`  
**Prior analysis:** `2026-05-16T1028` — many prior findings (intent guard, commit policy, skip-confirmations, review model routing) have been adopted. This analysis focuses on the evolved skill.

---

## Skill understanding

`bmad-epic-dev` is a **build orchestrator** that drives a multi-story epic from backlog to done by delegating to `bmad-create-story` (Phase A), `bmad-dev-story` + `bmad-code-review` (Phase B) via fresh subagents. It parses a dependency DAG from the epic's story table, parallelizes story specs in Phase A via concurrent subagents and development in Phase B via **git worktrees** (`best-of-n-runner` subagents), and enforces **adversarial code review** using a different LLM model. `sprint-status.yaml` is the sole checkpoint for resume; the `epic-plan.py` script provides deterministic pre-parsing.

**Primary users:** Builders managing multi-story epics in BMad-enabled repos — ranging from solo developers to team leads orchestrating a sprint's work.

**Key assumptions:** git is available and worktree-capable; subagent infrastructure (including `best-of-n-runner`) is operational; epic files contain well-formed markdown story tables with dependency columns; review-spawned stories eventually terminate the spec→dev→review loop; the user can resolve ambiguous decisions relayed from subagents.

---

## User journeys

### First-timer

**Narrative:** A developer new to BMad sees the epic in sprint-status and types "implement epic 16." They've never seen a subagent, a worktree-based build, or adversarial review with model routing.

**Friction points:**
- **Worktree concept is unexplained.** The execution plan mentions "git worktrees" and "best-of-n-runner subagents" — both are platform internals that mean nothing to a newcomer. They'll wonder why their IDE shows extra directories and whether it's safe.
- **Review model selection is daunting.** If `--review-model` wasn't configured, auto-selection happens silently — but the plan says "Review model: cursor-grok-4.6-xhigh (auto-selected because dev model is claude-4.6-opus)." A first-timer may wonder: "Is that the right choice? Will it cost me money?" No guidance on what the model choice affects qualitatively.
- **Error messages from `epic-plan.py` are developer-facing.** If the script fails (wrong epic number, missing table), the JSON error like `{"status": "error", "errors": ["No stories table found in ..."]}` is not user-friendly. The orchestrator should translate this into actionable English.
- **Phase A review gate is powerful but sudden.** After parallel spec creation, the skill dumps a list of file paths and says "review these." A first-timer doesn't know *what* to look for in a story spec, how thorough to be, or what kinds of issues to flag. This is the most critical gate in the entire workflow and it has zero coaching.

**Bright spots:** Intent guard catches "13.2" misfire elegantly. The execution plan display (epic name, layers, parallelism) educates the user on the structure. Resume behavior means mistakes aren't catastrophic.

### Expert

**Narrative:** A power user has already reviewed specs, reviewed the dependency graph, and just wants to rip through Phase B at maximum throughput.

**Friction points:**
- **No phase-entry flag.** Even when all stories are `ready-for-dev`, the expert must sit through Step 0's execution plan display and approval. `--start-phase=B` or automatic detection ("All stories ready-for-dev — jumping to Phase B") would eliminate ceremony.
- **Phase A review gate is un-overridable.** `--skip-confirmations` suppresses soft gates, but the review gate is always enforced. An expert who pre-edited specs wants `--trust-specs` to auto-confirm.
- **No parallelism cap.** A 12-story epic with 6 stories in layer 1 spawns 6 simultaneous `best-of-n-runner` subagents. On a resource-constrained machine, this could thrash. No `--max-parallel=N` option.
- **Review fix cycle limit (2) is hard-coded.** An expert might know the review findings are minor and want `--max-review-cycles=3` without manual escalation.
- **No partial-layer resume.** If 4 of 6 stories in a layer completed before a halt, re-invocation processes the full layer again (the 4 done stories skip via status, but the orchestrator still re-builds the full plan and re-presents it). Experts want a silent "skipping 4 done stories, 2 remaining" rather than a full plan display.

**Bright spots:** `--commit-policy=skip` respects expert workflows. `--skip-confirmations` removes most ceremony. Worktree parallelism is genuinely fast.

### Confused user

**Narrative:** User types "dev epic 13" but meant "deploy epic 13" or wanted to just check status.

**Friction points:**
- **Intent guard only catches story vs. epic confusion** — not "dev vs. check status vs. deploy." A broader intent check ("This will create specs, write code, and commit changes for epic 13. Continue?") would catch more mismatches.
- **No "list available epics" helper.** If the user provides a wrong epic number, they get "No epic file found" — but no pointer to what epics *do* exist. A quick `ls` of epic files or sprint-status scan would transform this from a dead end into navigation.
- **Accidental second invocation.** If the user accidentally runs the skill again while subagents are in flight from a previous invocation, there's no lock or detection. Sprint-status could become inconsistent.

**Bright spots:** Epic `done` → halt is cheap. Intent guard for story references works well.

### Edge-case user

**Narrative:** A valid but unusual setup — stories with circular dependencies, epic files in non-standard locations, statuses that don't match expected values.

**Friction points:**
- **Circular dependencies produce orphan layers silently.** `build_dependency_layers` in `epic-plan.py` places unreachable stories in a final orphan layer without distinguishing "no dependencies left" from "circular dependency." The orchestrator processes them anyway — potentially out of order.
- **"Unknown" status stories fall through.** Stories not in sprint-status get `status: "unknown"`, which doesn't match any Phase A/B filter (`backlog`, `ready-for-dev`, `in-progress`, `review`, `done`). These stories are silently skipped — no warning, no explicit "story 13.7 not found in sprint-status."
- **Review-spawned story loop has no termination bound.** B.7 checks for new `backlog` stories and loops back to Phase A. If code review keeps spawning stories (e.g., a review finding that generates a "fix-XSS" story that itself generates more findings), the loop is unbounded. No max-iteration guard.
- **Multi-epic concurrent execution.** Nothing prevents running `bmad-epic-dev` for epic 13 and epic 14 simultaneously. Sprint-status is a shared file — concurrent writes could corrupt it.

**Bright spots:** DAG re-parse on resume handles partial completion elegantly. Story status cross-referencing is thorough for known states.

### Hostile environment

**Narrative:** Git worktree creation fails (locked index, permission issues), subagent infrastructure is degraded, disk is full, or network drops mid-model-call.

**Friction points:**
- **Worktree failure recovery is unspecified.** B.1 assumes `best-of-n-runner` creates worktrees successfully. If worktree creation fails (e.g., branch already exists from a previous crashed run, or `.git/worktrees` is locked), the failure surfaces as a subagent error with no orchestrator-level guidance.
- **Merge conflict resolution is a cliff.** B.4 says "halt with details, user resolves manually, then re-invokes." But: what state is the story in? (`review` — since B.4 happens after review approval.) What does the user do after resolving? (Re-invoke, and the story's `review` status means... it re-runs review? Or does manual merge mean they should mark it `done`?) The resume table doesn't have a "merge-conflict" state.
- **Model unavailability mid-epic.** Review model validated at Step 0, but if it becomes unavailable during Phase B (rate limit, outage), reviews fail with no retry or fallback strategy.
- **Script Python version mismatch.** `epic-plan.py` requires Python ≥3.9 but the shebang uses `/usr/bin/env python3` — on some systems this resolves to 3.6 or 3.8. The `# /// script` metadata block with `requires-python` isn't enforced by direct invocation.
- **Total subagent unavailability.** B.6 provides sequential fallback when `best-of-n-runner` isn't available, but if *no subagent types* are available (e.g., platform outage), the entire skill is inoperable with no degradation path. The skill could theoretically run inline (no subagents) for single-story epics.

**Bright spots:** Sequential fallback (B.6) is well-documented. Orphaned worktree detection on resume is a thoughtful touch.

### Automator (CI / chained agent)

**Narrative:** A CI pipeline invokes `bmad-epic-dev 16 --commit-policy=auto --skip-confirmations` and expects it to complete unattended or exit with a clear status.

**Friction points:**
- **Phase A review gate blocks headless execution.** This is the single biggest blocker for automation. `--skip-confirmations` doesn't override it. A `--headless` or `--auto-approve-specs` flag is needed.
- **Decision-needed relays require a human.** No escalation policy for automation — should it fail-fast, log-and-continue, or apply a default? A `--on-decision=fail` flag would make CI behavior predictable.
- **No structured exit artifact.** The skill's completion output is prose. A CI pipeline needs a machine-readable summary: `{ "epic": 16, "status": "done|halted", "stories_completed": [...], "stories_pending": [...], "failed_story": null, "review_model": "...", "commits": [...] }`.
- **No timeout mechanism.** A subagent that hangs (waiting for a model, stuck in a review loop) blocks the entire epic indefinitely. No `--timeout-per-story=30m` or similar guard.
- **Commit messages aren't configurable.** CI teams may need specific message formats (Conventional Commits with scope, Jira ticket IDs, etc.). The hardcoded `feat(epic-{N}): {story_key} — {title}` pattern may not match organizational requirements.

**Bright spots:** `--commit-policy` and `--skip-confirmations` cover most automation needs. Sprint-status as a checkpoint is inherently automation-friendly.

---

## Headless assessment

**Level: Easily adaptable.**

The skill has evolved significantly since the prior analysis. `--commit-policy`, `--skip-confirmations`, and checkpoint-driven resume are already in place. The remaining gaps are narrow and well-defined:

| Interaction Point | Current Behavior | Headless Resolution |
| --- | --- | --- |
| Plan approval (Step 0) | Asks unless `--skip-confirmations` | ✅ Already covered |
| Phase A batch reports | Informational | ✅ Already non-blocking |
| **Phase A review gate** | **Always mandatory** | ❌ Needs `--auto-approve-specs` or `--headless` flag |
| **Decision-needed relays** | **Always halt for human** | ❌ Needs `--on-decision=<fail\|log\|default>` policy |
| Orphaned worktree prompt | Asks user on resume | ❌ Needs `--clean-worktrees` or `--resume-worktrees` default |
| Ambiguous epic selection | Asks user to choose | ❌ Needs epic number as required arg (already is, but fallback to "pick one" is interactive) |

**Suggested output contract:**
```json
{
  "epic": 16,
  "status": "done|halted|error",
  "phase_reached": "A|B|completion",
  "stories": [
    { "key": "16-1", "status": "done", "review_verdict": "APPROVED", "review_cycles": 1, "commit_sha": "abc123" }
  ],
  "halted_at": { "story": "16.4", "reason": "merge_conflict", "resume_hint": "resolve conflict on branch epic-16/story-16.4, then re-invoke" },
  "review_model": "cursor-grok-4.6-xhigh",
  "artifacts": ["path/to/16-1.md", "path/to/16-2.md"]
}
```

**Assessment:** Three flags (`--headless`, `--on-decision=fail`, `--clean-worktrees`) plus a JSON exit summary would make this skill fully headless-capable. The state machine is already externalized in sprint-status.yaml; it's just the interaction points that need parameterization.

---

## Key findings

| Severity | Area | Observation | Concrete suggestion |
| --- | --- | --- | --- |
| **high-opportunity** | Phase A review gate / automation | The mandatory review gate in Phase A blocks all headless and CI use cases. `--skip-confirmations` explicitly doesn't override it. This is the single largest barrier to automation. | Add `--auto-approve-specs` (or umbrella `--headless`) flag. When set, skip the review gate but log: "Specs auto-approved (headless mode) — review before merging to main." Keep the gate mandatory in interactive mode. |
| **high-opportunity** | Review-spawned story loop bound | B.7 loops back to Phase A when code review spawns new stories, with no termination condition. A pathological review that keeps generating fix stories could loop indefinitely. | Add a `max_review_spawn_iterations` limit (default: 2). After the limit, halt with: "Review has spawned stories across {N} iterations — likely a design issue. Review manually and re-invoke." |
| **high-opportunity** | Merge conflict recovery gap | B.4 halts on merge conflicts with "user resolves manually" but the resume behavior table doesn't define a "merge-conflict" state. After manual resolution, the user doesn't know whether to mark the story `done`, re-run review, or just re-invoke. | Define a `merge-conflict` story state (or document that manual merge completion + marking `done` in sprint-status is the contract). On resume, detect a branch that's been merged but not cleaned up and auto-advance to cleanup. |
| **high-opportunity** | Phase-entry for experts/resume | Experts resuming a partially-complete epic, or automation targeting only Phase B, must still walk through Step 0's full plan and approval. No way to say "just start developing." | Add `--start-phase=<A\|B>` flag. Auto-detect when possible: if all stories are `ready-for-dev` or later, announce "All specs exist — proceeding directly to Phase B" without requiring a flag. |
| **medium-opportunity** | Circular dependency detection | `epic-plan.py` silently places circular-dependency stories in an orphan layer, indistinguishable from truly independent stories. The orchestrator processes them without warning, potentially in wrong order. | In `epic-plan.py`, detect when orphaned stories have unresolved incoming edges (i.e., they're in a cycle, not just independent). Report: `"warnings": ["Circular dependency detected: 16.3 ↔ 16.5 — placed in final layer"]`. The orchestrator should surface this as a user warning. |
| **medium-opportunity** | Unknown status stories | Stories in the epic file but missing from sprint-status get `status: "unknown"` and are silently skipped in both Phase A and Phase B. No warning is surfaced. | Treat `unknown` as `backlog` (include in Phase A) OR surface a warning: "Story 16.7 not found in sprint-status.yaml — adding as backlog" or "skipping — add it to sprint-status to include." Either way, make it visible. |
| **medium-opportunity** | Phase A review gate coaching | The review gate lists file paths but doesn't help users know *what to check*. First-timers rubber-stamp; experts want a checklist. | Add a brief coaching prompt: "Key things to verify: acceptance criteria are testable, task breakdown matches your understanding, dependencies are correct, and no scope creep from the original epic." Even 2-3 bullets would transform this gate from ceremony to value. |
| **medium-opportunity** | Structured exit artifact | Completion outputs prose: story counts, review patches, commits. No machine-readable artifact for CI, dashboards, or chained skills. | Write a `{implementation_artifacts}/epic-{N}-summary.json` on completion (and on halt, with `halted_at` details). Include story statuses, review verdicts, commit SHAs, and review model used. |
| **medium-opportunity** | Model availability mid-epic | Review model is validated at Step 0, but could become unavailable during Phase B (rate limits, outages). No retry or fallback. | Add retry logic (3 attempts with backoff) for review model failures. If exhausted, offer fallback: "Review model unavailable — proceed with same model (reduced adversarial value) or halt?" In headless mode, fall back automatically with a warning. |
| **medium-opportunity** | Concurrent epic guard | Nothing prevents running `bmad-epic-dev` for two epics simultaneously. Sprint-status is a shared YAML file — concurrent writes could corrupt it. | At Step 0, write a lightweight lock (`{implementation_artifacts}/.epic-dev-lock` with PID and epic number). Check for existing locks before proceeding. Clear on completion or halt. Stale lock detection via PID check. |
| **low-opportunity** | Parallelism cap | Wide dependency layers could spawn many simultaneous subagents (e.g., 8+ worktrees for a large epic). No resource control. | Add `--max-parallel=N` (default: 4). Process layers in batches when the layer width exceeds the cap. |
| **low-opportunity** | Commit message template | Hardcoded `feat(epic-{N}): {story_key} — {title}` may not match organizational Conventional Commits or Jira-linked formats. | Make commit message configurable via `_bmad/config.yaml` key `commit_message_template` with `{epic}`, `{story_key}`, `{title}` placeholders. Low priority — most teams are fine with the current format. |
| **low-opportunity** | Review artifact persistence | Review findings (verdicts, must-fix counts, review cycles) live only in subagent output and the orchestrator's memory. If compaction occurs, review history is lost. | Write `{implementation_artifacts}/{story_key}-review.md` with the review verdict, findings, and fix log. Useful for audit trails and retrospectives. |
| **low-opportunity** | Dry-run mode | No way to preview what the skill *would do* without executing — useful for validation, estimation, and CI dry runs. | `--dry-run` flag that runs Step 0 (parse, plan, present) and outputs the execution plan without spawning any subagents. Could also estimate duration based on story count and layer depth. |

---

## Top insights

1. **The Phase A review gate is the skill's most consequential design choice — and its biggest automation blocker.** It's the right default for interactive use (preventing runaway development on bad specs), but its un-overridable nature makes headless execution impossible. The fix is surgical: one flag (`--auto-approve-specs` or `--headless`) to bypass it, plus a log warning that specs were auto-approved. This single change unlocks CI pipelines, chained-agent orchestration, and scheduled builds — all high-value use cases for an epic-dev skill.

2. **Unbounded loops are the hidden risk in an otherwise robust state machine.** The review-spawned story loop (B.7 → Phase A → Phase B → B.7) and the review fix cycle (B.3, capped at 2 but with no cap on B.7 re-entries) create a theoretical path where the skill never terminates. Real-world probability is low, but the consequence is severe: subagent costs accumulate, git history fills with fix commits, and the user discovers the problem only when they check back hours later. A simple iteration counter (default 2, configurable) transforms this from an unbounded risk to a documented limit.

3. **Merge conflict recovery is the sharpest operational gap.** The skill handles every other failure mode with clear resume instructions, but merge conflicts — the most common real-world failure in a worktree-based parallel workflow — leave the user in an undocumented state. The story is marked `review` (passed), the branch has the code, but the merge failed. What now? Defining a `merge-conflict` state or documenting the exact manual recovery steps (resolve, complete merge, mark `done`, clean worktree, re-invoke) would prevent the most frustrating failure mode in the entire workflow.

---

## Facilitative patterns check

| Pattern | Present? | Assessment |
| --- | --- | --- |
| **Soft Gate Elicitation** | ✅ Present | Plan approval, phase transitions, and review gate all use soft gates. `--skip-confirmations` controls pacing. Well-implemented. |
| **Intent-Before-Ingestion** | ✅ Present | Intent guard added since last analysis — catches story-vs-epic misfire. Could be broader (dev vs. status-check vs. deploy) but adequate for the primary confusion vector. |
| **Capture-Don't-Interrupt** | ❌ Missing | When the user provides tangential input during orchestration (e.g., "oh also, story 16.3 should use Redis instead of Memcached"), there's no mechanism to capture it and route it to the relevant story's spec. Suggestion: maintain an "orchestrator notes" scratchpad that feeds into subsequent subagent prompts. **medium-opportunity** — would reduce information loss in long orchestration sessions. |
| **Dual-Output** | ❌ Missing | Only human-facing prose output. No LLM-optimized distillate or structured summary for downstream skills. The structured exit artifact suggestion (above) partially addresses this. **medium-opportunity** — becomes high if the skill is commonly chained. |
| **Parallel Review Lenses** | ✅ Delegated | Adversarial code review with a different model is a strong parallel-lens implementation. The blind review contract (reviewer gets only diff + spec, no dev context) is excellent. Review runs in parallel across stories in a layer. |
| **Three-Mode Architecture** | Partial | Two modes exist: interactive (default) and reduced-ceremony (`--skip-confirmations`). A true `--headless` mode (auto-approve specs, fail-fast on decisions, structured output) would complete the trifecta. **high-opportunity** — the skill is 80% of the way there. |
| **Graceful Degradation** | ✅ Mostly present | Sequential fallback (B.6) for when `best-of-n-runner` is unavailable. Review model fallback (same model with warning). Config defaults when `_bmad/config.yaml` is missing. Gap: no degradation if *all* subagents are unavailable — total platform outage means total skill failure. **low-opportunity** — edge case, and inline execution would be a major complexity addition. |

**Most valuable adds for this skill (in priority order):**
1. **Three-Mode Architecture completion** — `--headless` flag encompassing auto-approve-specs + fail-fast decisions + structured output
2. **Capture-Don't-Interrupt** — orchestrator notes scratchpad for mid-flow user input
3. **Dual-Output** — structured epic summary artifact (`epic-{N}-summary.json`)

---

## Evolution since last analysis (2026-05-16)

Notable improvements adopted from the prior enhancement-opportunities analysis:

| Prior Finding | Current Status |
| --- | --- |
| Intent guard (high-opportunity) | ✅ Implemented — intent guard in SKILL.md catches story-vs-epic confusion |
| Commit policy (high-opportunity) | ✅ Implemented — `--commit-policy=<auto\|ask\|skip>` as a first-class arg |
| Skip confirmations (high-opportunity) | ✅ Implemented — `--skip-confirmations` flag suppresses soft gates |
| Review model routing | ✅ Implemented — `--review-model` arg with auto-selection fallback |
| Parallel Phase B | ✅ Implemented — git worktrees via `best-of-n-runner` subagents |
| Adversarial review isolation | ✅ Implemented — separate subagent, different model, diff-only context |

The skill has matured substantially. The remaining opportunities are in automation hardening (headless mode, loop bounds, merge conflict recovery) and polish (coaching at the review gate, structured output, notes capture).

---

*End of enhancement-opportunities analysis.*
