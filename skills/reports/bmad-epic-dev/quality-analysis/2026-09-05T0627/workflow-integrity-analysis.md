# Workflow Integrity Analysis — bmad-epic-dev

## Assessment

The skill is structurally sound and well-architected. SKILL.md serves as a clean entry point with correct frontmatter, required sections, and config integration, while `references/phase-logic.md` carries the full execution logic with explicit phase boundaries, progression conditions, resume behavior, and subagent contracts. The only structural note is a pre-pass type classification mismatch — the pre-pass detected `simple-utility` because there are no numbered stage files at root, but the skill actually implements a complex multi-phase orchestration workflow via its reference file. This is a valid design choice (the orchestrator needs the full plan in context), not a deficiency.

## Key Findings

### Medium

**1. Pre-pass type classification mismatch**
- **File:** Pre-pass output (`workflow-integrity-prepass.json`, `metadata.workflow_type`)
- **What's wrong:** The pre-pass classified this skill as `simple-utility` based on the absence of numbered stage files at root and the minimal SKILL.md structure. The skill is actually a complex orchestration workflow — it has multi-phase logic (Step 0, Phase A, Phase B with sub-phases B.1–B.7), dependency-layer parallelism, resume behavior with a state table, and review cycles with bounded iteration. All of this complexity lives in `references/phase-logic.md` rather than in numbered root-level stage files.
- **Impact:** Other tooling or scanners relying on the pre-pass type may apply the wrong checks (e.g., checking for "input/output format" instead of "progression conditions").
- **Fix:** This is informational for the pre-pass algorithm — the skill structure itself is valid. The pre-pass could be enhanced to detect complex workflows in `references/` files, or the skill could add a `workflow_type: complex` field to frontmatter to override detection.

### Low

**2. `user_name` resolved but never referenced in workflow logic**
- **File:** `SKILL.md:39`
- **What's wrong:** On Activation resolves `user_name` from config, but neither SKILL.md nor `references/phase-logic.md` ever uses it. It may be implicitly passed to subagents through config or used by sub-skills directly, but it creates a dead-reference appearance in this skill's own logic.
- **Fix:** Either remove `user_name` from the resolve list if sub-skills load config independently (they do — each sub-skill has its own On Activation config loading), or add a comment clarifying that it's resolved for potential use in user-facing messages.

**3. No explicit iteration bound on Phase A ↔ B.7 loop**
- **File:** `references/phase-logic.md:330–337` (B.7 steps 16–18)
- **What's wrong:** After completing all stories, B.7 checks for new `backlog` stories (spawned by code review) and loops back to Phase A. There is no explicit max iteration count for this outer loop. The B.3 fix-pass has a clear 2-cycle limit, but the Phase A → B → B.7 → Phase A loop does not. In practice this is bounded (code review won't spawn infinite stories), but the omission is inconsistent with the explicit bounding elsewhere.
- **Fix:** Add an explicit max loop count (e.g., 3 iterations) with an escalation-to-user message, matching the pattern already used in B.3's 2-cycle limit.

## Strengths

1. **Contract anchor pattern** — The "Contract anchor (survives context compaction — re-read if uncertain)" block at the top of `phase-logic.md` places critical invariants where they resist context window pressure. Strong defensive design.

2. **Adversarial review isolation** — The dev/review separation is rigorously specified: separate subagents, different LLM models, no dev history in review context. The contract is stated three times (SKILL.md, contract anchor, B.2 header) — redundancy is intentional and appropriate for a critical invariant.

3. **Deterministic pre-pass script** — `scripts/epic-plan.py` offloads dependency graph parsing and topological sorting from the LLM. The script includes proper error handling, cycle detection (orphan placement), and structured JSON output. It even has a test file (`scripts/tests/test-epic-plan.py`).

4. **Comprehensive resume behavior** — The state table in the Resume Behavior section maps every story state to a clear action. The dependency graph is re-parsed on resume, and orphaned worktree branches are detected with user guidance.

5. **Mandatory review gate** — The Phase A → Phase B gate (step 7) is always enforced, even with `--skip-confirmations`. This ensures human oversight before code generation begins.

6. **Consistent customization preamble** — Every subagent prompt template includes the TOML customization loading step (`_bmad/custom/<skill>.toml`), and the contract anchor explicitly requires this.

7. **Bounded fix cycles** — B.3 limits review/fix iterations to 2 cycles with escalation to user, preventing infinite review loops.

8. **Intent guard** — The On Activation check distinguishes between full epic orchestration and single-story references, preventing accidental full-epic runs.

9. **Clean file organization** — The skill has exactly 4 files with clear separation: SKILL.md (entry/overview), `references/phase-logic.md` (execution logic), `scripts/epic-plan.py` (deterministic computation), and `scripts/tests/test-epic-plan.py` (script tests). No orphaned or extraneous files.

10. **Well-formed frontmatter** — Name matches folder (`bmad-epic-dev`), description follows two-part format with quoted trigger phrases, no extra fields.
