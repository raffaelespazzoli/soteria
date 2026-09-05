# BMad Method · Quality Analysis: bmad-epic-dev

**Analyzed:** 2026-09-05T06:27Z | **Path:** `.cursor/skills/bmad-epic-dev`
**Interactive report:** quality-report.html

## Assessment

**Good** — This is an exceptionally well-architected orchestration skill with production-grade resume behavior, rigorous adversarial review isolation, and smart use of deterministic scripts. The primary opportunity is completing the automation story: three interaction points block headless execution, and the orchestrator's prompt layer carries ~1,000 tokens of template boilerplate that scripts could eliminate. Path reference issues (bare `_bmad` without `{project-root}`) need a quick fix.

## What's Broken

### 1. Bare `_bmad` references without `{project-root}` prefix
- **File:** `references/phase-logic.md:10` — `_bmad/custom/<skill>.toml`
- **File:** `references/phase-logic.md:47` — `_bmad/config.user.yaml`
- **What's wrong:** Two references use `_bmad/...` without the `{project-root}` prefix. In a subagent context where the working directory may differ, these resolve to the wrong location.
- **Fix:** Prefix both with `{project-root}/`: `{project-root}/_bmad/custom/<skill>.toml` and `{project-root}/_bmad/config.user.yaml`.

### 2. `uv` not found — Python linting unavailable
- **File:** `scripts/epic-plan.py`
- **What's wrong:** The environment lacks `uv`, so `ruff` cannot lint the Python script. This is an environment setup issue, not a skill defect.
- **Fix:** Install uv: https://docs.astral.sh/uv/getting-started/installation/

## Opportunities

### 1. Automation & Headless Readiness (high — 6 observations)

The skill is 80% of the way to headless-capable — `--commit-policy`, `--skip-confirmations`, and checkpoint-driven resume are already in place. But three interaction points still block unattended execution: the mandatory Phase A review gate, decision-needed relays with no escalation policy, and orphaned worktree prompts with no default action. A CI pipeline invoking this skill will always stall. Adding a `--headless` umbrella flag (or targeted `--auto-approve-specs`, `--on-decision=fail`, `--clean-worktrees`) plus a structured JSON exit artifact would unlock the highest-value automation use cases.

**Fix:** Implement a `--headless` flag that auto-approves specs (with a log warning), fails fast on decision-needed relays, auto-cleans orphaned worktrees, and writes an `epic-{N}-summary.json` on completion/halt.

**Observations:**

- Phase A review gate is un-overridable — blocks all CI/headless use — `references/phase-logic.md:106-116` (enhancement-opportunities)
- No structured exit artifact — completion is prose only — `references/phase-logic.md:342-355` (enhancement-opportunities)
- Decision-needed relays have no automation policy — `references/phase-logic.md:96-97` (enhancement-opportunities)
- No phase-entry flag for experts resuming — `references/phase-logic.md:56-60` (enhancement-opportunities)
- Orphaned worktree prompt has no default action — `references/phase-logic.md:371-373` (enhancement-opportunities)
- Three-Mode Architecture incomplete (interactive + reduced-ceremony exist, headless missing) — (enhancement-opportunities)

### 2. Orchestrator Context Bloat from Templates & Read-Ahead (medium — 7 observations)

The orchestrator reads four large subagent prompt templates (~600 tokens), reproduces them with variable substitution, and pre-reads story specs + diffs (~1,000–3,000 tokens per story) before delegating to review subagents. This bloats the parent's context window proportionally to layer width. A `render-prompt.py` script could eliminate the template text entirely, and delegating spec/diff reading to review subagents would prevent parent context growth.

**Fix:** Build a `render-prompt.py` script that stores templates, accepts key-value args, and outputs fully rendered prompts. Refactor Phase B.2 to delegate diff extraction and spec reading to the review subagent.

**Observations:**

- ~600 tokens of subagent prompt template text the LLM reads and reproduces — `references/phase-logic.md:79-92,155-178,207-232,251-273` (script-opportunities)
- Customization preamble repeated verbatim 4× (~320 tokens) — `references/phase-logic.md:80-84,157-160,208-210,252-254` (script-opportunities)
- Parent reads diffs + specs before delegating to review subagents — `references/phase-logic.md:197-222` (execution-efficiency)
- Sequential per-story description before batch review launch — `references/phase-logic.md:196-234` (execution-efficiency)
- Subagent return formats not JSON-enforced (~50-200 tokens wasted per response) — `references/phase-logic.md:86-92,166-177,226-232` (execution-efficiency)
- Commit message templates could fold into prompt renderer — `references/phase-logic.md:119,174,269` (script-opportunities)
- Config files loaded without explicit parallel instruction — `SKILL.md:33-39` (execution-efficiency)

### 3. Unbounded Iteration Risks (medium — 4 observations)

While the review fix cycle (B.3) is capped at 2 iterations, higher-level loops lack termination guards. The review-spawned story loop (B.7 → Phase A → Phase B) can theoretically cycle indefinitely if reviews keep generating fix stories. Circular dependencies are silently absorbed into orphan layers without warning. Unknown-status stories are silently skipped. These share a root cause: the skill's state machine trusts that edge cases self-terminate rather than enforcing explicit bounds.

**Fix:** Add `max_review_spawn_iterations` (default: 2) for the B.7 loop. Detect circular dependencies in `epic-plan.py` and surface warnings. Treat unknown-status stories as `backlog` or warn explicitly.

**Observations:**

- Review-spawned story loop (B.7→A→B) has no termination bound — `references/phase-logic.md:330-337` (workflow-integrity, enhancement-opportunities)
- No explicit iteration bound on Phase A ↔ B.7 loop — `references/phase-logic.md:330-337` (workflow-integrity)
- Circular dependencies silently placed in orphan layer — `scripts/epic-plan.py` (skill-cohesion, enhancement-opportunities)
- Unknown-status stories silently skipped — `references/phase-logic.md:361-369` (enhancement-opportunities)

### 4. Reference Self-Containment Gaps (medium — 4 observations)

Phase-logic.md back-references SKILL.md for commit-policy semantics and uses bare `_bmad` paths (also covered in "What's Broken"). During long-running orchestration, context compaction could drop SKILL.md, leaving the orchestrator without the definition of `auto` vs `ask` vs `skip`. The model slug list is hardcoded with no maintenance pointer. The `user_name` config value is resolved but never referenced.

**Fix:** Add a 1-line commit-policy definition to the contract anchor in phase-logic.md. Add a maintenance note for the model slug list. Remove `user_name` from the resolve list or document its purpose.

**Observations:**

- Commit-policy semantics defined only in SKILL.md, not phase-logic.md — `references/phase-logic.md:14` (prompt-craft)
- Model slug list hardcoded with no canonical-source note — `references/phase-logic.md:49-52` (prompt-craft)
- `user_name` resolved but never referenced — `SKILL.md:39` (workflow-integrity)
- Skip-confirmations behavior not defined in contract anchor — `references/phase-logic.md:14` (prompt-craft)

### 5. Merge Conflict Recovery Gap (high — 3 observations)

The skill handles every failure mode with clear resume instructions — except merge conflicts, the most common real-world failure in a worktree-based parallel workflow. When a merge in B.4 fails, the story is marked `review` (passed), the branch has the code, but the resume table doesn't define what happens next. Users are left in an undocumented state with no guidance on whether to revert, resolve forward, or manually mark `done`.

**Fix:** Define a `merge-conflict` story state (or document exact manual recovery: resolve conflict, complete merge, mark `done`, clean worktree, re-invoke). Add partial-merge rollback guidance for multi-story layers.

**Observations:**

- No merge-conflict state in resume table — `references/phase-logic.md:361-369` (enhancement-opportunities)
- No partial-merge rollback guidance for multi-story layers — `references/phase-logic.md:285-295` (skill-cohesion)
- Worktree failure recovery unspecified — `references/phase-logic.md:143-148` (enhancement-opportunities)

## Strengths

1. **Adversarial review isolation is architecturally enforced.** The dev/review separation uses different subagent types, different LLM models, and zero context leakage. The contract anchor restates this at the top of phase-logic.md where it survives context compaction. This is the gold standard for automated code review.

2. **`epic-plan.py` is a model script integration.** Offloading DAG computation, topological sort, and status cross-referencing to a deterministic Python script saves hundreds of LLM tokens and eliminates non-determinism. The "script-first, LLM-fallback" gate at phase-logic.md line 22 is the pattern all skills should follow.

3. **Resume behavior is production-grade.** Sprint-status.yaml as the sole checkpoint, with story states mapping directly to resume positions, is elegant and robust. Orphaned worktree detection on resume handles real-world interrupted sessions.

4. **Progressive disclosure is exemplary.** SKILL.md at 44 lines / 715 tokens is remarkably lean for an orchestrator managing specs, parallel dev, adversarial review, and fix cycles. All detail lives in `references/phase-logic.md` — loaded once, not on every skill scan.

5. **Contract anchor designed for compaction survival.** The clearly marked constraint block at the top of phase-logic.md places non-negotiable rules (orchestrator-not-implementer, fresh subagents, dev/review separation) where they resist context window pressure during long orchestrations.

6. **Dependency-layer parallelism is carefully reasoned.** Phase A parallelizes markdown specs (no conflicts). Phase B parallelizes via git worktrees (separate working directories). Sequential merge in B.4 maintains linear history. This reflects genuine understanding of when parallelism is safe.

7. **Intent guard prevents expensive mistakes.** Catching "13.2" (single story) before launching a multi-hour orchestration is a small but high-value interaction pattern.

8. **Bounded review fix cycles with escalation.** The 2-cycle cap in B.3 acknowledges that persistent disagreements are design questions, not code fixes — a mature engineering judgment.

9. **Self-contained subagent templates.** Each template includes customization loading, story context, specific instructions, output format, and scope guards. Subagents execute without referencing external files.

10. **Fail-forward within a layer.** Allowing in-flight dev subagents to complete while preventing new layers from starting avoids wasting already-running work — a sophisticated concurrency strategy.

## Detailed Analysis

### Structure & Integrity

The skill is structurally sound with clean file organization: SKILL.md (entry/overview), `references/phase-logic.md` (execution logic), `scripts/epic-plan.py` (deterministic computation), and `scripts/tests/test-epic-plan.py` (script tests). Frontmatter is well-formed with name matching folder. The pre-pass classified the skill as `simple-utility` due to the absence of numbered stage files, but the skill actually implements complex multi-phase orchestration via its reference file — a valid design choice that the pre-pass algorithm could be enhanced to detect.

**Remaining findings:**
- Pre-pass type classification mismatch (`simple-utility` vs. actual complex orchestration) — informational for pre-pass algorithm improvement.

### Craft & Writing Quality

**Overview quality:** Strong. The 23-line Overview establishes mission, domain framing, and role clarity with minimal token cost. The core contract section sets behavioral constraints that directly enable informed autonomy.

**Progressive disclosure:** Excellent. The heaviest token investment (phase-logic.md at ~375 lines) is loaded once per activation, not on every skill scan. `epic-plan.py` (~265 lines) handles what scripts should handle.

**Remaining findings:**
- Customization preamble repetition across templates is structural necessity, not waste — each template runs in isolated subagent context. (Note — acknowledged as well-designed by prompt-craft scanner.)

### Cohesion & Design

**Assessment:** Highly cohesive with strong stage flow coherence, purpose alignment, and complexity appropriateness. Every phase builds on prior outputs, parallelism strategy is carefully reasoned, and the adversarial review contract is the most rigorous design element.

**Dimension scores:**
- Stage flow: **strong** — linear top-level with well-structured sub-phases, no dead-end stages, unambiguous entry points
- Purpose alignment: **strong** — promises match behavior, design principles consistently honored
- Complexity: **strong** — complexity matches the task's decision space, script reduces mechanical work
- Gap/redundancy: **moderate** — execution plan staleness after Phase A gate, cross-platform skill resolution fragility

**Remaining findings:**
- Execution plan may be stale after Phase A review gate — re-run `epic-plan.py` after user confirms to catch edits (`references/phase-logic.md:106-116`)
- Cross-platform skill resolution: sub-skills in `.claude/skills/` resolved by name only from `.cursor/skills/` — fragile if discovery mechanism changes (`references/phase-logic.md`)
- B.6 sequential fallback positioned after B.5 but is an alternative, not successor — cosmetic positioning issue (`references/phase-logic.md:310-326`)
- No concurrent-epic guard — sprint-status.yaml is a shared file (`references/phase-logic.md`)

### Execution Efficiency

**Assessment:** Well-optimized. Dependency-layer parallelism, deterministic Python pre-pass, worktree-based isolation, and strict orchestrator-not-implementer contract keep the parent lean. The main efficiency concern — parent accumulating diffs and specs — is addressed in Opportunity #2.

**Remaining findings:** None beyond what's covered in Opportunity #2 (context bloat theme).

### User Experience

**Journeys:**

- **First-timer:** Execution plan display educates on structure, intent guard catches misfires. Friction: worktree concept unexplained, review model selection daunting, Phase A review gate has no coaching on what to check.
- **Expert:** `--commit-policy=skip` and `--skip-confirmations` respect power-user workflows. Friction: no phase-entry flag, review gate un-overridable, parallelism cap missing, review cycle limit hard-coded.
- **Confused user:** Epic `done` → halt is cheap, intent guard works well. Friction: no "list available epics" helper, no broader intent check (dev vs. status vs. deploy).
- **Edge-case user:** DAG re-parse on resume handles partial completion. Friction: circular deps silent, unknown-status stories silent, multi-epic concurrent execution unguarded.
- **Hostile environment:** Sequential fallback (B.6) well-documented. Friction: worktree creation failure unspecified, merge conflict recovery is a cliff, model unavailability mid-epic has no retry.
- **Automator:** Sprint-status checkpoint is automation-friendly. Friction: Phase A gate blocks headless, no structured exit, no timeout mechanism.

**Headless assessment:**
- **Potential:** Easily adaptable
- **Notes:** Three targeted flags (`--headless`, `--on-decision=fail`, `--clean-worktrees`) plus a JSON exit summary would make the skill fully headless-capable. The state machine is already externalized in sprint-status.yaml; only interaction points need parameterization.

### Script Opportunities

**Assessment:** The existing `epic-plan.py` is well-designed and covers the heaviest deterministic block. However, ~1,970 tokens of additional deterministic work remain in the prompt layer — prompt template rendering, review model resolution, sprint-status verification, git command construction, and worktree detection. A phased script buildout would eliminate this tax.

**Token savings:** ~1,970 estimated total across 9 opportunities. Highest-ROI: `render-prompt.py` (~920 tokens combining prompt rendering + customization preamble + commit messages).

**Remaining findings:**
- Sprint-status verification at 3+ checkpoints could be scripted (~250 tokens) — `references/phase-logic.md:100,189,297,330`
- Git diff command construction with exclude patterns (~150 tokens) — `references/phase-logic.md:198-199,318`
- Git merge + cleanup command sequence (~120 tokens) — `references/phase-logic.md:285-295`
- Resume state→action mapping is a pure lookup table (~100 tokens) — `references/phase-logic.md:361-369`

## Recommendations

1. **Add `--headless` automation mode** — resolves 6 observations across automation, CI, and expert journeys. Low-medium effort: three flag behaviors + JSON exit artifact. Highest impact because it unlocks entirely new use cases.

2. **Build `render-prompt.py` for subagent prompt rendering** — resolves 4 observations in the context bloat theme, saves ~920 tokens per invocation. Medium effort: new script with template storage and variable substitution. High reuse potential across orchestrator skills.

3. **Add iteration bounds to all loops** — resolves 4 observations. Low effort: `max_review_spawn_iterations` config (default 2), circular dependency detection in `epic-plan.py`, unknown-status story warning.

4. **Fix path references and self-containment gaps** — resolves 4 observations. Very low effort: add `{project-root}` prefix to 2 bare paths, add commit-policy definition to contract anchor, note model slug maintenance source.

5. **Define merge conflict recovery** — resolves 3 observations. Low effort: document recovery procedure, consider adding `merge-conflict` state to sprint-status, add partial-merge guidance.

6. **Extend `epic-plan.py` with verification modes** — resolves 4 script findings (~550 tokens). Low effort: `--resolve-review-model`, `--check-worktrees`, per-story `required_action` fields. Natural extensions of existing script.
