# Workflow Integrity Analysis: `bmad-epic-dev`

**Scanner:** Workflow Integrity (unified structural + wiring)  
**Skill path:** `.claude/skills/bmad-epic-dev`  
**Files reviewed:** `SKILL.md`, `references/phase-logic.md`  
**Pre-pass note:** `workflow-integrity-prepass.json` reported `workflow_type: "simple-utility"` with zero issues; manual review disagrees with that classification (see below).

## Assessment

The skill is structurally sound for day-to-day use: frontmatter follows naming rules, `SKILL.md` has `## Overview` and `## On Activation` with config loading, and the primary executable instructions live in `references/phase-logic.md`, which exists and is correctly referenced. The main gaps are **classification / pattern alignment** (orchestration + external reference doc does not match the scanner’s “simple utility” or “complex workflow with numbered root stage files” templates) and **metadata brevity** for the description’s summary clause.

## Key findings

### High — Pre-pass workflow type vs. actual structure (`workflow-integrity-prepass.json` vs. `SKILL.md` + `references/phase-logic.md`)

**What’s wrong:** Automated pre-pass labeled the skill `simple-utility`. The content is a **multi-phase orchestration workflow** (input validation, dependency DAG, Phase A parallel spec creation, Phase C sequential implement/review/commit, resume table, completion). That mismatch means tooling that trusts `workflow_type` may skip complex-workflow checks or apply the wrong ruleset.

**How to fix:** Update the pre-pass classifier to treat “primary logic in `references/*.md` with phases” as at least a **simple workflow** or **complex workflow variant**, or add an explicit workflow label in `SKILL.md` (e.g. a short `## Workflow type` line) so both humans and scripts classify it consistently.

### Medium — Description summary length (`SKILL.md:3`)

**What’s wrong:** Scanner guidance expects the description’s first clause to be a **5–8 word** summary before the trigger. The first sentence is longer (~11 words before “Use when…”), which can dilute the “short hook” that drives conservative triggering.

**How to fix:** Shorten the first clause, e.g. “Orchestrates end-to-end epic development.” then keep the rest of the detail in the second sentence or rely on `## Overview` for elaboration.

### Medium — Complex-workflow conventions without root stage files (`SKILL.md`, `references/phase-logic.md`)

**What’s wrong:** The integrity checklist for **complex workflows** expects numbered stage prompts at the skill root (`01-…md`) referenced from `SKILL.md`, plus per-stage config headers (language / document language). This skill instead delegates everything to a **single reference document**. That is a valid authoring pattern but diverges from the documented BMad/workflow-builder convention, so automated “stage file” checks will always report “no stages” even though behavior is staged.

**How to fix:** Either (a) split `phase-logic.md` into numbered root stage files and reference them from `SKILL.md`, or (b) document this pattern as an allowed variant and extend scanners to honor `references/` phase docs.

### Medium — No language / output-language header in phase reference (`references/phase-logic.md:1`)

**What’s wrong:** For multi-step external prompts, the scanner expects an explicit config header using `{communication_language}` and, where docs are emitted, `{document_output_language}`. The phase file jumps straight into “Step 0” without restating config-bound language behavior.

**How to fix:** Add a short preamble block under the title instructing the executor to communicate in `{communication_language}` and to match artifact language to `{document_output_language}` when writing or editing files.

### Low — Phase letter gap (A then C) (`references/phase-logic.md:36`, `references/phase-logic.md:78`)

**What’s wrong:** Phases jump from **A** to **C** with no **B**, which can confuse readers and downstream docs that assume contiguous lettering.

**How to fix:** Add a one-line note (“Phase B is reserved / unused”) or renumber to A/B for spec vs implementation if that reflects the real lifecycle.

## Strengths

- **Frontmatter:** `name` matches folder `bmad-epic-dev` and follows `bmad-{code}-{skillname}` (`SKILL.md:2`).
- **Trigger hygiene:** Description uses quoted, explicit invocation phrases (`SKILL.md:3`).
- **Required skeleton:** `## Overview` (`SKILL.md:8`) and `## On Activation` (`SKILL.md:23`) are present and ordered logically; role is clear (“disciplined build orchestrator”, `SKILL.md:12`).
- **Config integration:** On Activation loads `_bmad/config.yaml` and `config.user.yaml` and resolves key paths and preferences (`SKILL.md:25–31`).
- **Wiring:** `./references/phase-logic.md` exists and is the single hop for execution (`SKILL.md:32`).
- **Progression and completion:** `phase-logic.md` defines goals, ordering (DAG / topological sort), user gates, failure halts with resume instructions, and a completion section (`references/phase-logic.md:169–177`).
- **Template hygiene:** No orphaned `{if-*}` blocks or bare `{displayName}`-style placeholders in the skill tree.
- **No forbidden exit sections:** No `## On Exit` / `## Exiting` in `SKILL.md`.

---

**Severity summary:** 1 High (classification / tooling alignment), 3 Medium (description brevity, pattern vs conventions, language headers in reference), 1 Low (phase lettering).
