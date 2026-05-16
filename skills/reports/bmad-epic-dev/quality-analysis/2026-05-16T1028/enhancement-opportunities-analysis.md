# Enhancement Opportunities Analysis: `bmad-epic-dev`

**Scanner:** BMad Workflow Builder — Creative Edge-Case & Experience Innovation (`quality-scan-enhancement-opportunities.md`)  
**Skill path:** `.claude/skills/bmad-epic-dev`  
**Artifacts reviewed:** `SKILL.md`, `references/phase-logic.md` (no additional root-level `*.md` prompt files present)

---

## Skill understanding

`bmad-epic-dev` is an **orchestration skill**: it does not author specs or code itself; it parses an epic’s story table and `sprint-status.yaml`, orders work via a dependency DAG, and delegates to `bmad-create-story`, `bmad-dev-story`, and `bmad-code-review` in fresh subagents. Primary users are **builders running multi-story epics** in BMad-enabled repos. Core assumptions: BMad config and artifact paths exist or degrade to defaults; **git** is available and **automated commits** in Phase A/C are acceptable; the user can answer **decision-needed** relays from subagents; and **linear epic progression** (with resume via checkpoint files) matches how work actually happens on one branch.

---

## User journeys

### First-timer

**Narrative:** User hears “dev this epic” and invokes the skill with vague wording; may not know epic numbers, where `sprint-status.yaml` lives, or what a subagent is.

**Friction:** Step 0 resolves a single backlog epic automatically — magic when it works, **opaque when multiple epics** share `backlog` (must choose without much coaching). No upfront **orientation** (what will happen, how long, what they must provide). **Git commit steps** assume comfort with staging/commits and may clash with personal rules (“never commit unless asked”). Dependency parsing errors (missing table, odd filenames) are **not preemptively explained** in the skill — user discovers failure late.

**Bright spots:** Explicit **resume contract** and sprint-status as ground truth reduce fear of losing progress; **dependency layers** communicated in the plan educate newcomers on ordering.

### Expert

**Narrative:** User wants only Phase C (everything already `ready-for-dev`), or wants maximum throughput and minimal chatter.

**Friction:** **Soft gates** (“Ready to proceed?”, “Ready to begin implementation?”) add ceremony after every Phase A batch boundary and at kickoff. **Phase C is intentionally sequential** — experts optimizing wall-clock time hit an assumption wall (clean working tree). No documented **“spec-only”** or **“implement-only”** entry without manually editing sprint-status or invoking other skills directly.

**Bright spots:** Parallel **Phase A** batching respects the DAG; topological ordering is **transparent**.

### Confused user

**Narrative:** User meant “implement one story” or “review my PR,” but triggered epic orchestration.

**Friction:** No **intent checkpoint** before heavy parsing — wrong skill feels expensive (many subagent spawns). Accidental invocation with a **random epic number** could mutate the wrong epic’s status if files align.

**Bright spots:** Epic `done` → halt is a **cheap escape** for completed work.

### Edge-case user

**Narrative:** Valid but unusual input: epic file only under planning artifacts, **multi-dependency strings**, stories added mid-flight, or sprint-status **out of sync** with the epic markdown table.

**Friction:** Epic file discovery is heuristic — **ambiguous matches** aren’t specified (first hit vs conflict resolution). **Circular dependencies** in the story table aren’t mentioned. **Spawned stories** from review loop back to Phase A — powerful but **surprising** if not framed up front. Contradiction between “story marked done” and missing artifacts is **not a defined recovery path**.

**Bright spots:** **Re-parse DAG on resume** and defer undeveloped dependencies handles partial completion elegantly.

### Hostile environment

**Narrative:** Missing `_bmad/config.yaml`, corrupted `sprint-status.yaml`, read-only filesystem, **no parallel subagents**, or git hooks failing commits.

**Friction:** Config missing → defaults + pointer to setup — OK — but **invalid YAML** or git failures mid-epic leave the orchestrator in **unclear state** (committed locally vs status updated). Parallel fallback to sequential exists; **no fallback** if subagents are unavailable entirely (would need inline delegation instructions).

**Bright spots:** Sequential fallback for Phase A is explicitly documented.

### Automator (CI / chained agent)

**Narrative:** Pipeline supplies epic id and wants **non-interactive completion** or JSON exit summary.

**Friction:** Multiple **human confirmation gates** and **decision-needed** relays assume a human in the loop. Commit messages and `git add` patterns are imperative — **no documented dry-run** or **no-commit mode**. No **machine-readable summary artifact** (path/JSON) as a contract.

**Bright spots:** **Checkpoint-driven resume** is inherently automation-friendly if confirmations could be skipped or parameterized.

---

## Headless assessment

**Level:** **Partially adaptable.**

**Rationale:** The workflow’s **state machine is largely externalized** (`sprint-status.yaml`, story files, git), which suits automation. However, **explicit user prompts** at Step 0 (plan approval), Phase A completion (“Ready to begin implementation?”), and implicit **relay loops** for subagent decisions are **fundamentally interactive** unless parameterized.

**Interaction points that could auto-resolve:**

| Point | Headless parameter idea |
| --- | --- |
| Plan approval after Step 0 | `--yes` / `skip_plan_confirmation` |
| Phase A batch reporting | Stream logs only; optional `--quiet` |
| Post–Phase A commit | `--no-commit` or defer to caller |
| Decision-needed halts | Cannot auto-resolve without policy — need **fail-fast vs escalate-to-log** |
| Per-story commit (C.3) | `--single-commit-at-end` for automation |

**Suggested output contract (if extended):** Exit with structured summary, e.g. `{ "epic": N, "phase": "A|C|done", "failed_story": "13.2|null", "artifacts": [...], "resume_hint": "..." }`, plus non-zero status on halt.

**Stages needing explicit input even headless:** **Ambiguous epic selection**, **policy decisions** from subagents, and **irreconcilable parse errors** (unless inputs include exact file paths).

---

## Key findings

| Severity | Area | Observation | Concrete suggestion |
| --- | --- | --- | --- |
| **high-opportunity** | Intent & mis-invocation | Workflow jumps into epic validation without confirming the user meant **full epic orchestration** vs single-story or review-only. | Add a **one-line intent gate**: “Full epic (spec → implement → review) for epic N? If you only need one story, use `bmad-dev-story`.” Offer **abort with pointers** to sibling skills. |
| **high-opportunity** | User rules / commits | Phase A and C prescribe **git commits**; many users (and Cursor rules) forbid commits unless explicitly requested — **silent conflict** with `bmad-epic-dev` as written. | Add **`commit_policy`**: default follow phase-logic; **`defer-commits`** batches changes and lists suggested commits; **`confirm-each-commit`** aligns with cautious users. |
| **high-opportunity** | Expert fast-path | Experts doing resume-only Phase C still hit **confirmation prompts** designed for first-run planning. | **`--resume-silent`** or “If epic already `in-progress` and only Phase C remains, skip plan approval unless `--confirm`.” |
| **medium-opportunity** | Success / next steps | Completion summarizes counts but not **what to do next** (open PR, run CI, tag release). | Add **optional closure checklist**: link to PR skill, `make test`, deployment notes — even as templated bullets. |
| **medium-opportunity** | Parallelism expectation | Phase C sequentialism is documented in references but **SKILL.md overview** emphasizes parallelization — experts may expect parallel implementation. | One sentence in **Overview**: “Parallelism applies to **story specs** (Phase A); **implementation is sequential** per branch.” |
| **medium-opportunity** | Abrupt halts | Epic `done` halts with “nothing to do” — **no celebration or artifact index**, feels like a dead end for humans. | Micro-celebration + **index of story files and commits** for the epic. |
| **medium-opportunity** | Tangents / compaction | Long sessions risk **context compaction** dropping batch boundaries or user decisions; not acknowledged. | Short **phase banner** each turn (“Epic 13 · Phase C · Story 13.4”) and **re-state checkpoint file** after compaction-prone steps. |
| **medium-opportunity** | Facilitation — capture | Users often dump constraints mid-flow; workflow doesn’t say **capture-and-defer** for out-of-scope notes. | In orchestrator instructions: append **orphan notes** to an epic scratch section or `_bmad/` intake file for later stories. |
| **low-opportunity** | Dual-output | Downstream agents might want a **compact epic distillate** after completion. | Optional **`epic-{N}-distillate.md`** (or invoke `bmad-distillator`) — low priority if rarely chained. |
| **low-opportunity** | Three-mode architecture | Only one interaction density today. | **`guided` / `yolo` / `batch`** modes mapping to confirmation density — nice for power users, more maintenance. |

---

## Top insights

1. **Commit assumptions are the sharpest hidden edge:** The skill’s automation strength (checkpoint + commits) collides with common **human policies** about git; treating commits as configurable preserves trust without losing automation.

2. **Headless value is real but gated:** State in YAML/git is already automation-shaped; **confirmation ceremony** and **decision relays** are the main blockers — a small parameter surface (`skip_confirmations`, `commit_policy`, structured exit) would unlock CI/chained-agent use.

3. **Intent-before-ingestion is the biggest missing facilitative fix:** A lightweight **“wrong skill?”** routing moment prevents expensive misfires and improves perceived intelligence more than extra topology explanation.

---

## Facilitative patterns check

| Pattern | Present? | Notes |
| --- | --- | --- |
| **Soft Gate Elicitation | Partial | Plan approval and Phase A boundary questions exist; not consistently **“anything else before we continue?”** style at phase transitions. |
| **Intent-Before-Ingestion | Missing | High-opportunity: begins with epic/file validation, not **why** the user invoked epic-dev. |
| **Capture-Don’t-Interrupt | Missing | No mechanism for **deferring** tangential user input during orchestration. |
| **Dual-Output | Missing | Human-facing markdown artifacts only; no optional **LLM distillate** for chaining. |
| **Parallel Review Lenses | Delegated | Assumed inside `bmad-code-review`; epic-dev doesn’t add extra lenses at orchestration layer — acceptable. |
| **Three-Mode Architecture | Missing | Single pacing; experts and novices share the same gates. |
| **Graceful Degradation | Partial | Sequential fallback when parallel unavailable; **no** documented degradation if subagents cannot run at all. |

**Most valuable adds for this skill:** **Intent-before-ingestion**, **commit-policy / confirmation modes**, and **headless parameters** tied to existing checkpoints.

---

*End of enhancement-opportunities analysis.*
