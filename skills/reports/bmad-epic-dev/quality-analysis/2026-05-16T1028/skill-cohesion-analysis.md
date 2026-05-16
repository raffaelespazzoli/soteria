# Skill Cohesion Analysis: `bmad-epic-dev`

**Scanner:** Skill Cohesion & Alignment (`quality-scan-skill-cohesion.md`)  
**Skill path:** `.claude/skills/bmad-epic-dev`  
**Artifacts reviewed:** `SKILL.md`, `references/phase-logic.md`  
**Note:** No separate stage prompt files (`*.md` at skill root) and no workflow manifest with `after` / `before` / `is-required` were present; cohesion was inferred from the monolithic phase document and activation instructions.

---

## Assessment

`bmad-epic-dev` reads as a **coherent orchestration skill**: one clear identity (backlog → specs → implement → review → commit), a single checkpoint model (`sprint-status.yaml`), and a disciplined delegation pattern (fresh subagents, decision relay, halt-with-resume). The heavy lifting lives in `phase-logic.md`, which keeps `SKILL.md` lean and avoids duplicated rules.

The main tension is **not internal stage logic** but **alignment with repo-wide agent rules**: the phase logic mandates commits at phase boundaries without an explicit user opt-in gate, which can clash with projects whose rules say “only commit when asked.” That is a policy coherence issue worth clarifying in the skill, not a broken flow.

Overall verdict: **cohesive and executable** as written, with **medium** severity gaps around naming (Phase A → C), commit policy vs. user expectations, and the absence of machine-readable dependency metadata for workflow engines.

---

## Cohesion Dimensions

| Dimension | Score | Explanation |
|-----------|-------|-------------|
| **Stage flow** | **Strong** | Validate/parse → plan → Phase A (specs by DAG layer) → Phase C (topological implement/review/commit) → completion; loop for review-spawned stories is explicit and closes the loop. |
| **Purpose alignment** | **Moderate** | Stated orchestrator role (delegate, track, relay) matches `phase-logic.md`. “Interactive: soft gates” is partially honored (plan confirmation; Phase A asks before implementation) but automatic commits are a strong assumption vs. typical “ask before commit” guidance. |
| **Complexity appropriateness** | **Strong** | One reference file + short activation block is proportional to “full epic” scope; parallelism is scoped (A parallel, C sequential) with rationale. |
| **Completeness / gaps** | **Moderate** | Core path is covered; optional clarity gaps on Phase B naming, pre-flight checks (e.g., story file path conventions), and explicit sync with `bmad-dev-story` / `bmad-code-review` output contracts. |
| **Redundancy** | **Strong** | Little overlap between `SKILL.md` (contract + config) and `phase-logic.md` (procedure). |
| **Dependency graph logic** | **Moderate (N/A in part)** | Story-level DAG is well specified; there is **no** skill-level `after`/`before`/`is-required` graph—fine for prompt-only execution, weaker if a runner expects structured workflow metadata. |
| **External skill integration** | **Strong** | Clear trio: `bmad-create-story` → `bmad-dev-story` → `bmad-code-review`; `bmad-bmb-setup` for missing config. Subagent prompts state autonomy and halt conditions consistently. |

---

## Key Findings

### 1. Phase labeling skip (A → C)

- **Severity:** Low  
- **Area:** Stage flow / UX  
- **Issue:** Readers expect Phase B between A and C; the doc jumps numbering without explaining the omission (reserved for future work, merged phases, etc.).  
- **Improvement:** Add a one-line note (e.g., “Phase B is intentionally unused / reserved” or rename Phase C to “Phase B” for linear labeling).

### 2. Commit instructions vs. user-driven commit norms

- **Severity:** Medium  
- **Area:** Purpose alignment / design rationale vs. execution  
- **Issue:** Phase A (step 7) and Phase C (step 8) instruct unconditional `git commit` after batches/stories. Many workspaces instruct agents to commit only when the user asks; the skill’s “Core contract” does not reconcile this.  
- **Improvement:** State explicitly that this skill **requires** autonomous commits for checkpointing, or add a config flag / user confirmation before each commit while keeping `sprint-status.yaml` as the logical checkpoint.

### 3. `git add` scope inconsistency between Phase A and C.3

- **Severity:** Low  
- **Area:** Handoff / reproducibility  
- **Issue:** Phase A suggests adding “all new/modified files”; Phase C uses `git add -A`. Behavior differs (untracked ignored vs. full tree).  
- **Improvement:** Standardize on one policy per phase with a short rationale (e.g., epic-wide vs. story-scoped staging).

### 4. Subagent prompt vs. overview: “no user interaction” vs. “decision relay”

- **Severity:** Low  
- **Area:** Purpose alignment  
- **Issue:** Create-story prompts say “no user interaction needed” but the overview requires relaying decisions—consistent if read as “no interaction *unless* blocking,” but the create-story wording could make subagents over-eager to guess.  
- **Improvement:** Align wording with dev-story (“do not make assumptions”) for create-story where ambiguity exists.

### 5. No structured workflow dependency metadata

- **Severity:** Low (Suggestion for tooling)  
- **Area:** Dependency graph logic  
- **Issue:** Runners that consume YAML/graph workflows cannot derive ordering from markers alone.  
- **Improvement:** Optional sidecar manifest mapping phases to inputs/outputs if BMad adds structured execution later.

### 6. Epic source file discovery ambiguity

- **Severity:** Medium  
- **Area:** Completeness / failure modes  
- **Issue:** Multiple search paths (`implementation_artifacts` vs `planning_artifacts`) and glob patterns could yield wrong epic file if naming diverges; failure mode when table parse fails is not spelled out.  
- **Improvement:** Explicit validation (exactly one epic match, readable Stories table) and halt message template.

---

## Strengths

- **Resume model** is concrete: table mapping story states to Phase A/C actions; re-parse DAG on resume.  
- **Parallelism** is justified and bounded (Phase A by layer; Phase C sequential with future worktree note).  
- **Failure handling** includes halt messages with resume hints and epic-level stop on subagent failure.  
- **Spawned stories** loop (C.4 → back to A) closes a real-world gap without leaving orphan backlog items.  
- **Separation of concerns:** activation/config in `SKILL.md`, procedural truth in `references/phase-logic.md`.

---

## Creative Suggestions

1. **Dry-run mode:** Emit the execution plan and DAG as markdown without spawning subagents—useful for sprint reviews.  
2. **Explicit “Phase B” slot:** Optional human/architecture gate before implementation for high-risk epics (even if default no-op).  
3. **Telemetry block:** Append a short structured summary to `_bmad-output/` per epic run for retrospectives (ties naturally to `bmad-retrospective`).  
4. **Commit strategy hook:** Read a project convention file (e.g., AGENTS.md or `_bmad/config.yaml` flag) to choose commit granularity without forked skills.

---

*End of cohesion scan.*
