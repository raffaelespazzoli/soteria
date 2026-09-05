# Skill Cohesion Analysis: bmad-epic-dev

**Scanner:** SkillCohesionBot
**Skill path:** `.cursor/skills/bmad-epic-dev`
**Date:** 2026-09-05

---

## Assessment

`bmad-epic-dev` is a **highly cohesive, well-architected orchestration skill**. Its stage flow is logical and purposeful — every phase builds on the prior phase's outputs, the parallelism strategy is carefully reasoned (markdown specs in Phase A, git worktrees in Phase B), and the adversarial review contract is the most rigorous design element in the entire skill. The skill fulfills its stated purpose of driving an epic from backlog to done while maintaining clean separation of concerns: it never writes code or specs itself, delegating everything to specialized sub-skills via fresh subagents. Resume behavior, checkpoint tracking, and failure halting are all well-considered. This is a mature, production-oriented orchestration design.

---

## Cohesion Dimensions

### 1. Stage Flow Coherence — **Strong**

The flow is clean and linear at the top level (Step 0 → Phase A → Phase B → Completion) with well-structured sub-phases within Phase B (B.1 dev → B.2 review → B.3 fix → B.4 merge → B.5 report → B.7 new-story check). Key observations:

- **Earlier stages produce exactly what later stages need.** Step 0 produces the execution plan (dependency layers, story states) that Phase A and B consume. Phase A produces story spec files that Phase B.1 dev subagents read. Phase B.1 produces diffs that B.2 review subagents receive.
- **No dead-end stages.** Every phase produces artifacts or state updates consumed downstream. Even B.5 (report) serves the user-interaction contract.
- **Entry points are unambiguous.** Step 0 is always first. Resume behavior doesn't create alternate entry points — it uses the same flow but skips completed work.
- **B.7 (new-story loop) elegantly closes the cycle.** Review-spawned stories loop back through Phase A and B without special-casing.

One minor incoherence: **B.6 (Sequential Fallback)** is structurally positioned _after_ B.5 in the document, but it's actually an alternative execution mode for B.1–B.5, not a stage that runs after B.5. This positional ambiguity is cosmetic — the instructions are clear — but could confuse a naive reader or a weaker model.

### 2. Purpose Alignment — **Strong**

The skill's stated purpose ("orchestrate full epic development end-to-end") is precisely what the phases deliver. The design principles are consistently honored:

- **"You are an orchestrator, not an implementer"** — Enforced throughout. Every coding, spec-writing, and review action is delegated to subagents. The contract anchor at the top of phase-logic.md survives context compaction.
- **"Fresh subagent per sub-skill"** — Every subagent prompt template starts clean. No session state leaks between dev and review.
- **"Different model for review"** — Step 0.7 resolves the review model with a clear fallback chain (arg → config → auto-select → warn-and-proceed). Phase B.2 enforces this with explicit model specification.
- **"Halt on failure with resume instructions"** — Every failure path in B.1, B.2, B.3, and B.4 includes halt-and-report behavior.

The **promises-vs-behavior check** passes. The Overview says "interactive by default: report progress after each story, soft gates between phases" — and the phase logic delivers this with the Phase A review gate (mandatory) and per-layer progress reports (B.5). The `--skip-confirmations` flag suppresses soft gates but never the Phase A review gate, which is explicitly "always."

### 3. Complexity Appropriateness — **Strong**

This is a genuinely complex orchestration task: dependency graph resolution, topological sorting, parallel execution with git worktrees, adversarial multi-model review, fix cycles with escalation limits, resume from checkpoint, and dynamic new-story detection. The skill's complexity matches the task's decision space.

- **The Python script (`epic-plan.py`) is a smart complexity reducer.** Offloading deterministic DAG computation to a script avoids burning LLM tokens on parsing and sorting — a pattern more skills should adopt.
- **2-cycle review cap** prevents unbounded loops while still allowing iteration.
- **Fail-forward within a layer** (other dev subagents complete even if one fails) is a sophisticated concurrency pattern that avoids wasting already-running work.

The only question is whether Phase B.6 (sequential fallback) adds too much duplication. It's necessary (not all environments support `best-of-n-runner`), but it re-specifies the dev→review→fix→commit cycle with slightly different mechanics (e.g., `git diff HEAD~1..HEAD` vs. `git diff main..branch`). This is a reasonable trade-off — the alternative (abstracting both modes) would add indirection that might confuse execution.

### 4. Gap & Redundancy Detection — **Moderate**

**Gaps identified:**

- **No execution plan staleness check after Phase A review gate (Medium).** The execution plan is computed in Step 0 and the dependency layers are fixed. During the mandatory Phase A review gate, the user is invited to "edit any story file now." If the user adds stories, changes dependencies, or alters scope, the execution plan is stale. Phase B would proceed with the original DAG. The skill should re-run `epic-plan.py` (or re-parse) after the Phase A gate to catch any changes.

- **No partial-merge rollback in B.4 (Low).** If a layer has 3 stories approved and the merge of story 2 conflicts, story 1 is already merged. The skill halts correctly, but there's no guidance on whether the user should revert story 1's merge or resolve the conflict with story 1 already integrated. A brief note on this scenario would help.

- **Circular dependency handling is silent (Low).** `epic-plan.py` handles cycles by dumping orphaned stories into a final layer. But phase-logic.md never mentions this edge case. If stories have circular deps, they'd be attempted last with unsatisfied dependencies — likely failing. The orchestrator should detect and warn about cycles in Step 0.

- **No timing or duration tracking (Suggestion).** For an orchestration that can run for hours across many stories, tracking per-story and per-phase durations would help users estimate remaining time and identify bottlenecks. The completion report could include wall-clock time.

**Redundancy assessment:** Minimal. B.6 (sequential fallback) overlaps with B.1–B.4 by necessity. The subagent prompt templates are similar across phases but not identical — each includes context-specific instructions. No consolidation needed.

### 5. Dependency Graph Logic — **Strong**

This skill doesn't use formal YAML `after`/`before` markers (it's not a YAML-defined workflow), but the implicit dependency logic is sound:

- **Step 0 must complete before anything else** — it produces the execution plan consumed by all subsequent phases.
- **Phase A must complete before Phase B** — dev subagents need the story spec files created by Phase A.
- **B.1 must complete before B.2** — review needs the diff from development.
- **B.2 must complete before B.4** — merge requires review approval.
- **Layer N must complete before Layer N+1** — later layers depend on earlier layers' merged code.

The `sprint-status.yaml` checkpoint enables correct resume ordering — stories in earlier states are re-processed from the appropriate phase.

The **topological sort in `epic-plan.py`** (Kahn's algorithm) is correctly implemented and handles independent stories (single layer), linear chains, diamond dependencies, and orphans.

### 6. External Skill Integration Coherence — **Moderate**

The skill delegates to three sub-skills: `bmad-create-story`, `bmad-dev-story`, and `bmad-code-review`. All three exist in `.claude/skills/` and have clear interfaces. The integration is well-designed:

- **Each subagent prompt includes the customization preamble** — consistent pattern across all three sub-skills.
- **Output contracts are specified** — each prompt template tells the subagent exactly what to report back (story_key, status, summary, etc.).
- **Decision escalation is uniform** — all three sub-skills are instructed to halt on blocking decisions and report them for the orchestrator to relay.

**Concerns:**

- **Cross-platform skill resolution (Medium).** The `bmad-epic-dev` skill lives in `.cursor/skills/` while its three sub-skills live in `.claude/skills/`. The subagent prompts reference skills by name ("Run the bmad-dev-story skill"), relying on the agent platform to resolve them. This works today because the skills are registered in the agent's skill catalog, but it's fragile — if the skill discovery mechanism changes, or if the sub-skills are moved or renamed, the orchestrator's subagent prompts silently fail. At minimum, the skill should document this assumption. Ideally, prompts would include fallback paths.

- **Customization preamble vs. `resolve_customization.py` (Low).** The subagent prompts instruct the sub-skill to "check for and load `_bmad/custom/bmad-create-story.toml`." But `bmad-dev-story` and `bmad-code-review` both have their own customization resolution mechanism via `resolve_customization.py` (which merges `customize.toml` → team config → user config). When these skills run via epic-dev's subagent prompt, the preamble tells them to load the user `.toml` directly, but the skill's own activation step also runs `resolve_customization.py`. These shouldn't conflict (the skill's native resolution should take precedence), but the overlapping mechanisms could cause confusion if a future change alters the merge order.

- **`bmad-bmb-setup` mentioned but not integrated (Informational).** SKILL.md references `bmad-bmb-setup` for initial configuration, but it's just a suggestion to the user. This is fine — it's a setup concern, not an orchestration dependency.

---

## Strengths

1. **The adversarial review contract is exemplary.** The separation between dev and review subagents — different sessions, different models, zero context leakage — is the gold standard for automated code review. The contract anchor at the top of phase-logic.md, explicitly designed to survive context compaction, shows deep understanding of LLM failure modes.

2. **`epic-plan.py` is a brilliant complexity reducer.** Offloading deterministic DAG computation to a Python script is a pattern that saves significant LLM tokens and eliminates non-determinism in parsing. The script is well-tested (comprehensive unit tests covering edge cases), handles orphans gracefully, and produces clean JSON output. More orchestration skills should adopt this pattern.

3. **Resume behavior is production-grade.** Using `sprint-status.yaml` as the sole checkpoint, with story states mapping directly to resume positions, is elegant and robust. The orphaned-worktree detection on resume shows attention to real-world failure scenarios (interrupted sessions, crashed agents).

4. **Parallelism strategy is carefully reasoned.** Phase A parallelizes story specs (markdown files, no conflicts). Phase B parallelizes via git worktrees (separate working directories, no conflicts). Sequential merge in B.4 maintains linear history. This isn't accidental — it reflects genuine understanding of when parallelism is safe.

5. **The 2-cycle review cap with escalation is wise.** Unlimited review cycles would burn tokens and delay delivery. Capping at 2 and escalating to the user acknowledges that persistent disagreements are likely design questions, not code fixes. This is a mature engineering judgment.

6. **The intent guard prevents expensive mistakes.** Checking whether the user meant a full epic vs. a single story before launching a multi-hour orchestration is a small but high-value interaction pattern.

7. **Fail-forward within a layer is sophisticated.** Allowing other in-flight dev subagents to complete (avoiding wasted work) while preventing new layers from starting (avoiding cascading failures) is a nuanced concurrency strategy.

---

## Key Findings

| # | Severity | Area | Finding | Recommendation |
|---|----------|------|---------|----------------|
| 1 | Medium | Phase A → Phase B transition | Execution plan computed in Step 0 may be stale after the Phase A review gate. User edits during this gate (adding stories, changing deps) aren't detected. | Re-run `epic-plan.py` or re-parse the dependency table after the user confirms the Phase A review gate. Compare with the original plan and report any changes. |
| 2 | Medium | External Skill Integration | Sub-skills are resolved by name only, with no path fallback. The orchestrator and sub-skills live in different skill directories (`.cursor/` vs `.claude/`). | Document the assumption that sub-skills are discoverable via the agent's skill catalog. Consider adding fallback paths in subagent prompts (e.g., "The skill is located at `.claude/skills/bmad-dev-story/SKILL.md`"). |
| 3 | Low | Phase B.4 | No guidance for partial-merge scenarios. If story 2 of 3 conflicts during merge, story 1 is already merged. User needs to know whether to revert or resolve forward. | Add a note in B.4: "If a merge conflict occurs after earlier stories in the same layer were already merged, those merges are preserved. The user resolves the conflict on the conflicting branch and re-invokes to continue." |
| 4 | Low | Step 0 / `epic-plan.py` | Circular dependencies are silently handled by dumping orphans into a final layer. The orchestrator never warns the user. | Add a check in Step 0: if the execution plan contains an orphan layer (stories with unsatisfied deps), warn the user and ask whether to proceed or fix the dependency table first. |
| 5 | Low | Phase B.6 positioning | Sequential fallback (B.6) is positioned after B.5 in the document, but it's an alternative to B.1–B.5, not a successor. Positional ambiguity could confuse weaker models. | Move B.6 to a clearly separated section (e.g., an appendix or a "Fallback Mode" header at the same level as Phase B) to signal it's an alternative execution path, not a sequential step. |
| 6 | Suggestion | Completion report | No timing data. An epic orchestration can run for hours — users would benefit from knowing how long each phase, layer, and story took. | Track and report wall-clock durations: per-story dev time, review time, fix time; per-layer total; overall epic elapsed time. |
| 7 | Suggestion | Phase B.2 | Review subagent receives only the diff and spec, but not the project's testing conventions, architecture doc, or coding standards. The review quality depends on what `bmad-code-review`'s own activation pulls in. | Consider including a pointer to the project's architecture doc or coding standards in the review subagent prompt, so the reviewer can calibrate against project-specific conventions without relying solely on `bmad-code-review`'s own discovery. |
| 8 | Suggestion | Observability | No structured log or event stream. The user sees progress reports, but there's no machine-readable record of the orchestration run (which subagents ran, their outcomes, timing, review verdicts). | Consider emitting a structured JSON log (or appending to sprint-status.yaml) with orchestration events: subagent launches, completions, review verdicts, merge operations, and durations. This enables post-mortem analysis and process improvement. |

---

## Creative Suggestions

1. **Adaptive parallelism based on story size.** Currently, all stories in a dependency layer are parallelized equally. If story sizes vary dramatically (one story has 2 tasks, another has 15), the layer bottlenecks on the largest story while smaller stories' worktrees sit idle. The orchestrator could split large layers into sub-waves or provide the user with a parallelism budget (max concurrent subagents) to manage resource consumption.

2. **Pre-flight dependency validation against codebase.** Before launching Phase B, the orchestrator could do a quick sanity check: does the story spec reference files/modules that actually exist? This catches stale specs (e.g., files renamed since spec creation) before burning a full dev subagent cycle.

3. **Review learning loop.** Track review findings across stories in the same epic. If the reviewer keeps flagging the same pattern (e.g., missing error handling, inconsistent naming), inject that pattern as a "known concern" into subsequent dev subagent prompts. This turns adversarial review into a teaching signal within the epic.

4. **Epic complexity pre-assessment.** Before presenting the execution plan in Step 0, classify the epic's complexity (story count × average dependency depth × estimated task count) and suggest a commit policy. Simple epics → `auto`. Complex epics → `ask`. This would reduce the cognitive load of choosing the right flags.

5. **Dry-run mode.** A `--dry-run` flag that executes Step 0 and reports the full execution plan (layers, parallelism, estimated subagent count, review model) without launching any subagents. Useful for understanding what will happen before committing to a potentially long orchestration run.
