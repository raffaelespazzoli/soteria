# Script Opportunities Analysis: `bmad-epic-dev`

**Scanner:** ScriptHunter (`quality-scan-script-opportunities.md`)  
**Skill path:** `.claude/skills/bmad-epic-dev/`  
**Artifacts read:** `SKILL.md`, `references/phase-logic.md`

---

## Existing scripts inventory

There is **no `scripts/` directory** under `bmad-epic-dev`. The skill consists only of `SKILL.md` and `references/phase-logic.md`. All orchestration logic is expressed as markdown instructions for the LLM.

---

## Assessment

Overall **intelligence placement is skewed toward the LLM** for work that is largely mechanical: YAML parsing, filesystem discovery, markdown table extraction, graph algorithms, and merging structured state are all deterministic. The skill correctly delegates creative work (`bmad-create-story`, `bmad-dev-story`, `bmad-code-review`) to sub-skills, but **Step 0 and several checkpoint validations still ask the orchestrator LLM to perform heavy parsing and bookkeeping** that increases token cost, latency, and variance on every activation.

A **small CLI or Python pre-pass** (PEP 723 with `pyyaml`, optionally no third-party deps beyond stdlib + YAML) could emit a compact JSON payload (`epic_plan.json`) consumed by the LLM for narration and user prompts only — matching the “pre-processing for LLM steps” pattern from the scanner.

---

## Key findings

### 1. Epic resolution, sprint-status validation, markdown story-table parse, DAG, topological layers, and cross-reference merge

| Attribute | Detail |
| --------- | ------ |
| **Severity** | **High** |
| **Location** | `references/phase-logic.md` Step 0, approximately lines **3–32** (items 1–7) |
| **What the LLM does today** | Reads `sprint-status.yaml`, checks epic state, searches filesystem for epic markdown by ordered patterns, parses the **Stories** table (story id, title, dependency column), builds a DAG, computes topological order and depth layers, merges with per-story statuses from YAML, and prepares an execution summary. |
| **What a script would do** | Accept `--epic N`, `--implementation-artifacts`, `--planning-artifacts`, `--sprint-status`; load YAML; glob `{impl}/{N}-epic-*.md` then fallback search under planning; parse markdown tables with regex or a lightweight table parser; build adjacency list; run topological sort + layer grouping; output JSON: `{ epicStatus, stories[], layers[], phasesEstimate, resumeHints }`. |
| **Estimated LLM tax** | **Heavy (500+ tokens)** per run when epic files and `sprint-status.yaml` are large (the real repo file mixes extensive header comments with `development_status` — the LLM may ingest far more than structurally needed). |
| **Implementation** | Python (PyYAML / stdlib `yaml` if available) + `pathlib` + `re` or `csv`-style split for pipe tables |
| **Pre-pass for scanner** | **Yes** — same JSON could feed workflow-integrity or cohesion scanners as “ground truth” epic shape |
| **Standalone lint value** | **Yes** — validate DAG acyclicity, unknown dependency refs, orphan stories in YAML vs epic file |
| **Reuse across skills** | **High** — any BMad workflow that reads epic markdown + `sprint-status.yaml` could share it |
| **`--help`** | Strong fit — interface documented once; prompts reference `epic-plan --help` |

---

### 2. Merge and validate `_bmad/config.yaml` + `config.user.yaml` into resolved paths

| Attribute | Detail |
| --------- | ------ |
| **Severity** | **Medium** |
| **Location** | `SKILL.md` lines **23–30** |
| **What the LLM does today** | Loads two YAML files, merges semantics (user overrides), extracts `implementation_artifacts`, `planning_artifacts`, `user_name`, `communication_language`. |
| **What a script would do** | Deep-merge YAML keys (documented precedence), emit single JSON or export statements; fail fast on missing required keys. |
| **Estimated LLM tax** | **Moderate (100–300 tokens)** depending on config size |
| **Implementation** | Python or `yq` + small wrapper |
| **Pre-pass** | **Yes** — combine with finding #1 as one `bmad-context` tool |
| **Standalone** | **Yes** — CI check that paths exist |
| **Reuse** | **High** — most BMad skills mention the same config |

---

### 3. Post–Phase A batch verification of `sprint-status.yaml` mutations

| Attribute | Detail |
| --------- | ------ |
| **Severity** | **Medium** |
| **Location** | `references/phase-logic.md` lines **61–63** |
| **What the LLM does today** | After parallel `bmad-create-story` runs, verifies each story reached `ready-for-dev` and optionally that epic flipped to `in-progress`. |
| **What a script would do** | Post-condition checker: given epic id + list of story keys, assert YAML keys exist and values match expected enum; optional diff against snapshot taken before batch. |
| **Estimated LLM tax** | **Moderate (~150–400 tokens)** when many stories listed |
| **Implementation** | Python + YAML |
| **Pre-pass** | Partial — post-processing validation after LLM-driven Phase A |
| **Standalone** | **Yes** — “sprint-status invariant” linter |
| **Reuse** | **Medium** |

---

### 4. Verify story status transition after dev-story (expect `review`)

| Attribute | Detail |
| --------- | ------ |
| **Severity** | **Medium** |
| **Location** | `references/phase-logic.md` line **114** |
| **What the LLM does today** | Confirms `sprint-status.yaml` shows story in `review`. |
| **What a script would do** | `assert_status(epic, story_key, "review")` |
| **Estimated LLM tax** | **Light–moderate** per story (~80–200 tokens); accumulates over many stories in Phase C |
| **Implementation** | Bash + `yq` or Python |
| **Pre-pass** | No — **post-step gate** |
| **Standalone** | **Yes** |
| **Reuse** | **Medium** |

---

### 5. Detect newly appeared `backlog` stories for the epic after Phase C

| Attribute | Detail |
| --------- | ------ |
| **Severity** | **Medium** |
| **Location** | `references/phase-logic.md` lines **159–165** |
| **What the LLM does today** | Re-reads full sprint-status and reasons about which stories are **new** backlog entries for this epic. |
| **What a script would do** | Compare current YAML story keys under epic prefix to a saved set from workflow start, or diff `--before` / `--after` snapshots; output `{ newBacklog: [...] }`. |
| **Estimated LLM tax** | **Moderate (100–400 tokens)** |
| **Implementation** | Python |
| **Pre-pass** | **Yes** — avoids second full-file narrative pass |
| **Standalone** | **Low–medium** |
| **Reuse** | **Medium** |

---

### 6. Completion summary aggregates (counts, commits, review patches)

| Attribute | Detail |
| --------- | ------ |
| **Severity** | **Medium** |
| **Location** | `references/phase-logic.md` **Completion** section, lines **171–177** |
| **What the LLM does today** | Totals stories completed, review patches, commits; updates epic to `done` when all stories done. |
| **What a script would do** | Count stories with status `done` under epic from YAML; optionally `git log --oneline` with message filter `feat(epic-N)`; review patch counts might remain LLM-sourced unless stored in machine-readable story metadata — partial automation. |
| **Estimated LLM tax** | **Moderate (150–350 tokens)** |
| **Implementation** | Python + git subprocess |
| **Pre-pass** | Partial |
| **Standalone** | **Yes** for YAML-derived counts |
| **Reuse** | **Medium** |

---

### 7. Resume behavior lookup table (story state → action)

| Attribute | Detail |
| --------- | ------ |
| **Severity** | **Low** |
| **Location** | `references/phase-logic.md` lines **184–196** |
| **What the LLM does today** | Applies a fixed mapping from story status to phase routing. |
| **What a script would do** | Trivial: encode table as JSON/`switch` in code; given statuses, emit next actions — marginal token savings. |
| **Estimated LLM tax** | **Light (<100 tokens)** |
| **Implementation** | Embedded in same Python module as #1 |
| **Pre-pass** | Optional |
| **Standalone** | Low value alone |
| **Reuse** | Low |

---

## Aggregate estimated token savings

| Bucket | Per-invocation savings (rough order of magnitude) |
| ------ | --------------------------------------------------- |
| Step 0 bundle (#1) | **800–3,000+** tokens (dominant; scales with epic file + sprint-status size) |
| Config resolution (#2) | **100–300** |
| Phase gates (#3, #4, #5) | **200–800** combined across an epic |
| Completion (#6) | **150–350** |
| Resume table (#7) | **50–100** |

**Conservative total per full epic orchestration:** roughly **1,300–4,500** tokens that could shift to **zero LLM cost** if replaced by scripts (excluding subjective narration and user interaction).

---

## Recommendations (prioritized)

1. **Implement finding #1 first** — highest ROI; reducesneed for the LLM to absorb entire epic markdown and bloated sprint-status comment headers when only `development_status` and one epic section matter structurally.
2. **Bundle #2 into the same tool** — single `bmad epic-plan` entrypoint with `--help`.
3. Add **post-condition validators (#3, #4)** as optional `--verify` flags for CI or obsessive consistency.
4. Use **snapshot diff (#5)** when code review spawning stories is common.

---

## Notes

- The skill explicitly avoids implementing code itself; scripts here remain **orchestration aids**, not substitutes for `bmad-dev-story`.
- Token estimates are **relative LLM tax** for deterministic steps, not exact metering.
