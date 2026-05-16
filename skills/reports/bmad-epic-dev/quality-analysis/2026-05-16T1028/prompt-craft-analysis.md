# Prompt Craft Analysis: `bmad-epic-dev`

**Scanner:** PromptCraftBot (quality-scan-prompt-craft.md)  
**Skill path:** `.claude/skills/bmad-epic-dev`  
**Prepass:** `prompt-metrics-prepass.json` (2026-05-16)

---

## Assessment

**Skill type:** Complex, multi-phase orchestration workflow (backlog → story specs → implement/review/commit) with branching (resume, spawned stories), delegation to sub-skills, and user soft gates. This correctly leans toward **outcome + rationale + selective HOW**, which the Overview and `phase-logic` largely deliver.

**Overview quality:** Strong. `SKILL.md` lines 10–21 establish mission (“entire epic from backlog to done”), domain framing (orchestrator vs implementer; specialized skills), theory-of-contract via the bullet list (fresh context, relay decisions, `sprint-status.yaml` as checkpoint, interactive reporting). Length is appropriate (~6 sentences plus a tight bulleted contract — well under the “excessive Overview” threshold).

**Progressive disclosure:** Appropriate. `SKILL.md` stays lean (~33 lines, ~458 tokens per prepass) and defers the long procedure to `references/phase-logic.md`, avoiding inline tables/schemas in the activation surface.

**Synthesis:** Craft is generally strong for informed autonomy: rationale appears where it matters (e.g., why Phase C is sequential). The main gap is **context compaction survival**: almost all executable detail lives in the reference file without restating the non-negotiable orchestration contract, and there are **no separate root-level stage prompts** (prepass reports `total_prompts: 0`), so “stage prompt” checklist items apply mainly to `phase-logic.md` by analogy.

---

## Prompt Health Summary

| Metric (prepass + manual) | Value |
|---------------------------|--------|
| Files scanned (prepass) | 1 (`SKILL.md` only in automation) |
| Root `*.md` stage prompts (excluding `SKILL.md`) | 0 |
| `SKILL.md` YAML config header (`communication_language`, etc.) | No |
| `SKILL.md` / workflow progression cues | Yes (`On Activation` → load config → follow reference; `phase-logic` has phased goals and Completion) |
| `references/phase-logic.md` config header | No |
| `phase-logic.md` structured progression | Yes (numbered steps, phases, Completion, Resume table) |

**Interpretation:** Prepass “prompt health” counts reflect repo layout (no root stage `.md` files). Qualitatively, **`phase-logic.md` is the working prompt** and should be scored like a stage prompt for self-containment and optional config conventions.

---

## Key Findings

Ordered by severity.

### 1. Medium — Self-containment under context compaction

**Where:** `references/phase-logic.md` (file-level; notably lines 1–35 opening into Step 0 / Phase A)

**What:** The reference jumps into procedural steps without a short “anchor” block that restates the **core contract** already defined only in `SKILL.md` (e.g., every sub-skill in a fresh subagent; relay “decision needed” to the user; on failure halt with resume instructions; `sprint-status.yaml` as source of truth; soft gates between phases).

**Why it matters:** If `SKILL.md` is evicted from context during a long epic run, the executing agent may still have `phase-logic.md` but lose the orchestration invariants — risking wrong parallelism assumptions, skipping user gates, or mishandling halts/resume semantics.

**Fix:** Add a 3–6 line block at the top of `phase-logic.md` (after the title) mirroring the non-negotiable bullets from `SKILL.md` lines 16–21, without duplicating the full Overview prose.

---

### 2. Medium — Missing BMad-style config header on executable prompts

**Where:** `SKILL.md` lines 1–4 (frontmatter); `references/phase-logic.md` (no frontmatter)

**What:** Neither file uses a config header that establishes `{communication_language}` and output conventions for the executing agent. `SKILL.md` does instruct loading config and resolving `communication_language` (`SKILL.md` lines 25–30) but does not tie that variable to response formatting in a standard header block.

**Why it matters:** Other BMad stage prompts typically align language/output up front; without it, behavior depends on the model remembering a later bullet.

**Fix:** Optionally add minimal YAML frontmatter to `phase-logic.md` (or a one-line “Apply `{communication_language}` from loaded BMad config to user-facing messages” immediately under the new anchor block). Keep it short to avoid redundant activation cost if both files stay hot.

---

### 3. Low — Reference loading phrasing is slightly soft

**Where:** `SKILL.md` line 32

**What:** “Then read fully and follow `./references/phase-logic.md`” is clear but not paired with mandatory language like “Treat as the authoritative procedure for all phases.”

**Why it matters:** Minor risk of skimming under pressure; suggestive loading is flagged in the scanner when ambiguity could cause skipped steps.

**Fix:** One clause: “Load and execute all steps in order unless halted for user decision.”

---

### 4. Low — Phase labeling gap (A vs C)

**Where:** `references/phase-logic.md` lines 36–78 (`Phase A`), 78 (`Phase C`)

**What:** There is no Phase B; readers may wonder if content is missing.

**Why it matters:** Small cognitive friction; not execution-breaking if intentional.

**Fix:** One sentence in Phase C header: “Phase B is unused / reserved” or rename to “Phase 1 / Phase 2” if numbering is acceptable for your module conventions.

---

### 5. Note — Architecture: single reference vs multiple stage prompts

**Where:** Skill root layout (prepass `total_prompts: 0`)

**What:** The workflow is one large `references/phase-logic.md` rather than root-level `phase-a.md`, `phase-c.md`, etc.

**Why it matters:** Not inherently wrong; keeps routing simple. Selective loading per phase would reduce tokens on short invocations but adds routing glue.

**Severity:** Note only — no change required unless optimizing for compaction/token budgets further.

---

### 6. Note — No pruning anti-patterns detected

**Where:** `SKILL.md`, `references/phase-logic.md`

**What:** No weighted scoring formulas, calibration tables, or adapter proliferation for “natural” LLM judgments.

**Severity:** Note (positive).

---

## Strengths

- **Lean activation surface:** `SKILL.md` size and token estimate are excellent; Overview + On Activation separation is clear (`SKILL.md` lines 8–21 vs 23–32).
- **Mission and delegation model:** Explicit “you orchestrate, you don’t implement” prevents role drift (`SKILL.md` lines 12–13).
- **Concrete subagent prompts:** Fenced templates for `bmad-create-story`, `bmad-dev-story`, and `bmad-code-review` reduce improvisation errors (`references/phase-logic.md` lines 47–54, 88–96, 118–127).
- **Operational resilience:** Resume table and parallelism notes encode real constraints (`references/phase-logic.md` lines 184–202).
- **Outcome vs implementation balance:** Good rationale where procedure could be questioned — e.g., sequential Phase C justified by clean working tree (`references/phase-logic.md` lines 80–82).

---

## Prepass Cross-Check

- `skill_md_summary.line_count`: 33 — aligns with manual read; within multi-branch guideline threshold.
- `overview_lines`: 14 — includes bullets; still coherent as unified Overview.
- `total_waste_patterns`: 0 — consistent with scan (no obvious filler stacks).
- `total_back_references`: 0 — no “as above” coupling detected in `SKILL.md`.
