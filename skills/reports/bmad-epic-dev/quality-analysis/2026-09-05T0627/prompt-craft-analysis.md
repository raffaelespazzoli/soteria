# Prompt Craft Analysis — bmad-epic-dev

**Scanner:** PromptCraftBot · prompt-craft  
**Skill:** `bmad-epic-dev`  
**Date:** 2026-09-05  

---

## Assessment

**Skill type:** Complex workflow (multi-stage orchestration with branching, parallelism, and external delegation).

**Overview quality:** Strong. The SKILL.md Overview (23 lines) establishes mission ("drives an entire epic from backlog to done"), domain framing (dependency trees, parallelization, adversarial review), and role clarity ("You don't write code or specs yourself — you delegate"). The core contract section is particularly well-crafted — it sets behavioral constraints (fresh context per sub-skill, halt-on-failure, sprint-status as checkpoint) that directly enable informed autonomy during execution.

**Progressive disclosure:** Excellent. SKILL.md is 44 lines / ~715 tokens — remarkably lean for an orchestrator that manages spec creation, parallel development via git worktrees, adversarial code review with model isolation, and multi-cycle fix passes. All detailed phase logic lives in `references/phase-logic.md` (~375 lines), and deterministic parsing (YAML, dependency graphs, topological sort) is offloaded to `scripts/epic-plan.py` (~265 lines). This is textbook progressive disclosure.

**Synthesis:** This is one of the better-crafted complex workflow skills. The orchestrator pattern is well-suited to the skill type — SKILL.md establishes the mental model and constraints, phase-logic.md provides complete execution procedures with self-contained subagent templates, and the script handles what scripts should handle. The main improvement area is a minor self-containment gap where phase-logic.md back-references SKILL.md for arg semantics.

---

## Prompt Health Summary

| Metric | Count | Notes |
|---|---|---|
| Total prompt files (root `*.md`) | 0 | No stage prompts — skill uses SKILL.md → references/ pattern |
| Prompts with config header | 0/0 | N/A (no stage prompts) |
| Prompts with progression conditions | 1/1 | SKILL.md delegates to phase-logic.md which has explicit completion criteria |
| Reference files | 1 | `references/phase-logic.md` — functions as the execution document |
| Scripts | 1 | `scripts/epic-plan.py` — deterministic epic plan extraction |

**Note:** The skill's architecture is SKILL.md (activation + mental model) → single reference file (full execution logic) → script (deterministic pre-pass). There are no separate stage prompts because the workflow has a single activation path. This is appropriate for the skill's design.

---

## Key Findings

### 1. Self-Containment Gap: Commit-Policy Semantics Not in Phase-Logic

**Severity:** Medium  
**File:** `references/phase-logic.md:14`  
**Also relevant:** `SKILL.md:15-18`

**What's wrong:** Phase-logic.md line 14 says "Respect `--commit-policy`, `--skip-confirmations`, and `--review-model` args from SKILL.md" — an explicit back-reference. The valid values for `--commit-policy` (`auto|ask|skip`) and their distinct behaviors are defined only in SKILL.md (lines 15-18). Phase-logic.md references "respecting `--commit-policy`" at commit points (lines 118, 320) but never defines what each value means.

**Why it matters:** Epic orchestration is a long-running workflow. If context compaction drops SKILL.md during Phase B, the orchestrator loses the semantics of `auto` vs `ask` vs `skip` and must guess or ignore the policy. The `--skip-confirmations` and `--review-model` args are safer — `--skip-confirmations` is used contextually (line 59: "Unless `--skip-confirmations`, ask...") and `--review-model` is fully explained in Step 0 (lines 47-56).

**Fix:** Add a 2-3 line summary of commit-policy values to the contract anchor block at the top of phase-logic.md:

```markdown
- `--commit-policy`: `auto` (commit at prescribed points), `ask` (prompt user before each commit), `skip` (user handles commits manually)
```

This costs ~25 tokens and eliminates the compaction risk.

### 2. Phase-Logic Contract Anchor Could Include Skip-Confirmations Behavior

**Severity:** Low  
**File:** `references/phase-logic.md:14`

**What's wrong:** The contract anchor (lines 4-14) lists `--skip-confirmations` as an arg to respect but doesn't define what it suppresses. The behavior is clear from usage context (line 59: "Unless `--skip-confirmations`, ask...") and the name is self-documenting, but for compaction resilience, a one-line definition would be cleaner.

**Why it matters:** Minor. The arg name is self-documenting and the usage context is clear. This is a polish item, not a reliability concern.

**Fix:** Add to the contract anchor: `--skip-confirmations` suppresses soft-gate pauses between phases (mandatory gates like the Phase A review gate are unaffected).

### 3. Model Slug List May Drift

**Severity:** Low  
**File:** `references/phase-logic.md:49-52`

**What's wrong:** The available model slugs for review-model selection are hardcoded in phase-logic.md (lines 49-52). If the platform's available models change, this list becomes stale.

**Why it matters:** The model list is domain-specific knowledge the LLM genuinely wouldn't know — the scanner explicitly says NOT to flag domain-specific knowledge. However, a hardcoded list in a reference file creates a maintenance burden. If the list is sourced from platform documentation or configuration, a note about where the canonical list lives would help.

**Fix:** Add a brief comment noting where the model list is maintained, or reference a config key that could override it. Alternatively, accept this as a conscious design trade-off (the list is short and changes infrequently).

### 4. Customization Preamble Repetition Across Templates (Structural, Not Waste)

**Severity:** Note  
**Files:** `references/phase-logic.md:80-81, 157-158, 207-208, 251-252`

**Observation:** The `IMPORTANT — Team customizations` block appears in all four subagent prompt templates. This looks like repetition in the orchestrator's context (~4 × 40 tokens = ~160 tokens).

**Why this is NOT waste:** Each template is composed into a separate subagent prompt that runs in isolated context. The orchestrator reads all templates, but each subagent only receives its own. The repetition is structural necessity — removing it would require the orchestrator to inject the preamble dynamically, adding fragile logic for minimal token savings. Well-designed.

---

## Strengths

**1. Exemplary progressive disclosure architecture.** SKILL.md at 44 lines / 715 tokens is among the leanest orchestrator skills possible while still establishing a complete mental model. The heavy lifting lives in `references/phase-logic.md` where it belongs — loaded once per activation, not on every skill scan.

**2. Contract anchor designed for compaction survival.** Phase-logic.md opens with a clearly marked contract block (lines 4-14) that restates the non-negotiable constraints: orchestrator-not-implementer, fresh subagent per skill, dev/review separation, different review model, customization preamble requirement. This is exactly the pattern that prevents degraded behavior during long workflows.

**3. Excellent intelligence placement.** `epic-plan.py` handles everything that should be deterministic: YAML parsing, story table extraction, dependency graph construction, topological sort. The script outputs structured JSON that the LLM consumes without re-deriving. The prompt correctly says "run it to extract the execution plan as JSON (faster, deterministic)" with a manual fallback if the script is unavailable. This is the gold standard for script/prompt division.

**4. Adversarial review isolation is structurally enforced.** The skill doesn't just instruct "use a different model" — it architecturally separates dev and review into different subagent types (`best-of-n-runner` for dev, `generalPurpose` for review), requires a different model slug, and explicitly excludes dev context from review prompts. The bold callout box (lines 132-139) and template structure make it very difficult for the orchestrator to accidentally violate this constraint.

**5. Self-contained subagent templates.** Each template includes everything the subagent needs: customization loading, story context, specific instructions, explicit output format, and guards against scope creep (e.g., "DO NOT run code review"). A subagent receiving any of these templates can execute without referencing any other file.

**6. Resume behavior is clean and complete.** The status-to-action mapping table (lines 363-369) plus the dependency graph re-parse on resume means the skill handles partial completion gracefully. The orphaned worktree detection (line 373) is a thoughtful edge case that prevents silent state corruption.

**7. Intent guard prevents common misactivation.** SKILL.md line 41 catches the case where a user says "13.2" meaning a single story, not the full epic. This is theory-of-mind in action — anticipating user confusion and routing to the right skill.

**8. Appropriate use of emphasis for critical constraints.** Bold, caps, and callout boxes are reserved for genuinely high-stakes instructions (dev/review separation, model isolation, halt-on-failure). No defensive padding or "remember to" filler — just direct imperatives where violation has consequences.
