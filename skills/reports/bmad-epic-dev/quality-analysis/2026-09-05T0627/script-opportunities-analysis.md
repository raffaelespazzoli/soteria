# Script Opportunities Analysis — `bmad-epic-dev`

**Skill path:** `.cursor/skills/bmad-epic-dev`
**Scanner:** ScriptHunter — deterministic operation detection
**Date:** 2026-09-05

---

## Existing Scripts Inventory

| Script | Purpose | Coverage |
|--------|---------|----------|
| `scripts/epic-plan.py` | Parses sprint-status.yaml and epic source file; produces a JSON execution plan with dependency graph, topological layers, story states, and parallelism opportunities | Covers Step 0 items 1–6 of phase-logic.md (input validation, epic file discovery, stories table parsing, DAG construction, sprint-status cross-reference) |
| `scripts/tests/test-epic-plan.py` | Unit tests for epic-plan.py | Good coverage: sprint-status parsing, stories table parsing, dependency layers, key resolution, end-to-end |

The existing `epic-plan.py` is well-designed and handles the heaviest deterministic block in the skill. The phase-logic.md already gates on it (line 22: "If an `epic-plan.py` script exists… run it… Otherwise, perform the steps below manually."). This is a model pattern for the rest of the skill to follow.

---

## Assessment

The skill has made a strong first move by extracting the execution-plan computation into `epic-plan.py`, eliminating the single most token-expensive deterministic block. However, **significant deterministic work remains in the prompt layer** — particularly around subagent prompt rendering (four large boilerplate templates with variable substitution), review model resolution, git command construction, sprint-status state verification, and orphaned worktree detection. These operations are scattered across `references/phase-logic.md` and collectively cost an estimated **1,500–2,000 tokens per invocation** on work that scripts could handle with zero LLM tax. The highest-value opportunity is a prompt-rendering script that would eliminate ~600 tokens of template text the LLM currently reads, internalizes, and reproduces with variable substitution.

---

## Key Findings

### Finding 1: Subagent Prompt Rendering — Variable Substitution on Boilerplate Templates

**Severity:** High
**LLM Tax:** ~600 tokens (4 templates × ~150 tokens each)
**Affected files:**
- `references/phase-logic.md:79–92` — Phase A `bmad-create-story` subagent prompt template
- `references/phase-logic.md:155–178` — Phase B.1 `bmad-dev-story` subagent prompt template
- `references/phase-logic.md:207–232` — Phase B.2 `bmad-code-review` subagent prompt template
- `references/phase-logic.md:251–273` — Phase B.3 fix-pass subagent prompt template

**What the LLM is currently doing:** Reading four large prompt templates from the phase-logic instructions, mentally substituting variables (`{epicNum}`, `{storyNum}`, `{title}`, `{story_key}`, `{story_spec_content}`, `{diff_output}`, `{review_findings}`, `{project-root}`), and reproducing the filled templates when dispatching subagents. Each template includes the customization preamble (repeated 4 times), task instructions, and reporting requirements — all static text with slot-fills.

**What a script would do instead:** A `render-prompt.py` script that:
1. Stores each template as a named template (Python string templates or Jinja2)
2. Accepts template name + key-value pairs as arguments
3. Automatically checks for and injects the customization TOML file content inline (instead of telling the subagent to check for it)
4. Outputs the fully rendered prompt to stdout

```
python3 render-prompt.py create-story \
  --epic-num 13 --story-num 1 --title "Add auth middleware"
```

**Estimated token savings:** ~600 tokens per invocation. The LLM would replace the four template blocks with a single instruction: "Run `render-prompt.py <template> <args>` to generate the subagent prompt." The orchestration logic (when to launch, error handling, parallelism decisions) stays in the prompt — only the mechanical rendering moves to the script.

**Implementation language:** Python (string.Template or Jinja2 via PEP 723)
**Pre-pass potential:** Yes — could be chained with `epic-plan.py` to produce a complete batch of ready-to-dispatch prompts for an entire dependency layer
**Standalone value:** Moderate — useful for any skill that dispatches parameterized subagent prompts
**Reuse across skills:** High — the pattern of "render a prompt template with variables" applies to any orchestrator skill (e.g., `bmad-dev-story` dispatching sub-tasks)

---

### Finding 2: Review Model Resolution

**Severity:** Medium
**LLM Tax:** ~200 tokens
**Affected file:** `references/phase-logic.md:47–57`

**What the LLM is currently doing:** Following a multi-step decision tree to resolve which model to use for adversarial code review:
1. Check `--review-model` CLI arg
2. Check `_bmad/config.user.yaml` for `bmm.review_model`
3. Auto-select from a hardcoded list (excluding the current dev model)
4. Warn if no alternative available

This is a pure decision tree with no ambiguity — every branch is determined by inputs.

**What a script would do instead:** A `resolve-review-model.py` (or add a `--resolve-review-model` subcommand to `epic-plan.py`) that:
1. Accepts `--review-model` arg (optional), `--config-path` (optional), `--dev-model` (required)
2. Walks the precedence chain
3. Outputs JSON: `{"review_model": "claude-4.6-opus-high-thinking", "source": "config", "warning": null}`

**Estimated token savings:** ~200 tokens (the entire resolution block replaced by "Run `epic-plan.py --resolve-review-model --dev-model <model>`")
**Implementation language:** Python (extend `epic-plan.py`)
**Pre-pass potential:** Yes — could be added to the execution plan JSON output from `epic-plan.py`
**Standalone value:** Low — specific to this skill's review-model logic
**Reuse across skills:** Low — the model resolution logic is specific to `bmad-epic-dev`, though the pattern could generalize

---

### Finding 3: Sprint-Status State Verification

**Severity:** Medium
**LLM Tax:** ~250 tokens (across 3 locations)
**Affected files:**
- `references/phase-logic.md:100` — "After each batch, verify sprint-status updates (stories → `ready-for-dev`, epic → `in-progress`)"
- `references/phase-logic.md:189` — "Update `sprint-status.yaml`: mark each completed dev story as `review`"
- `references/phase-logic.md:297` — "Update `sprint-status.yaml`: mark each merged story as `done`"
- `references/phase-logic.md:330–337` — "Re-read `sprint-status.yaml`. Check if any new `backlog` stories appeared"

**What the LLM is currently doing:** At multiple checkpoints, re-reading sprint-status.yaml to verify that sub-skills updated story statuses correctly, checking for expected state transitions, and detecting new stories that code review may have spawned.

**What a script would do instead:** An `epic-status-check.py` (or extend `epic-plan.py` with a `--verify` mode) that:
1. Accepts the original execution plan JSON and a list of expected state transitions
2. Re-reads sprint-status.yaml
3. Reports: stories matching expected state, stories in unexpected state, new stories not in original plan
4. Outputs JSON: `{"verified": true, "unexpected": [], "new_stories": ["13-8-new-thing"]}`

**Estimated token savings:** ~250 tokens (verification logic at 3+ checkpoints)
**Implementation language:** Python (extend `epic-plan.py`)
**Pre-pass potential:** Yes — the "new story detection" at line 330 is a diff between original plan and current state
**Standalone value:** High — sprint-status integrity checking is valuable as an independent lint
**Reuse across skills:** Medium — any skill that modifies sprint-status.yaml could use this

---

### Finding 4: Git Diff Command Construction

**Severity:** Medium
**LLM Tax:** ~150 tokens
**Affected files:**
- `references/phase-logic.md:198–199` — Worktree diff: `git diff main..epic-{N}/story-{N.M} -- . ':(exclude)_bmad-output' ':(exclude)*.md'`
- `references/phase-logic.md:318` — Sequential fallback diff: `git diff HEAD~1..HEAD -- . ':(exclude)_bmad-output' ':(exclude)*.md'`

**What the LLM is currently doing:** Reading diff command templates, substituting epic/story numbers, and deciding between worktree vs. sequential mode to construct the correct git diff invocation. The exclude patterns are static but repeated.

**What a script would do instead:** A `story-diff.sh` (or Python) that:
1. Accepts `--epic`, `--story`, `--mode=worktree|sequential`
2. Constructs and executes the correct diff command (with standard exclude patterns)
3. Outputs the diff to stdout

```bash
./story-diff.sh --epic 13 --story 2 --mode worktree
```

**Estimated token savings:** ~150 tokens (two diff command blocks + exclude patterns)
**Implementation language:** Bash (simple command construction and execution)
**Pre-pass potential:** Yes — the diff output is fed directly into the review subagent prompt
**Standalone value:** Medium — useful for manually inspecting story diffs
**Reuse across skills:** Low — the exclude patterns are project-specific, but the pattern generalizes

---

### Finding 5: Orphaned Worktree Detection

**Severity:** Medium
**LLM Tax:** ~150 tokens
**Affected file:** `references/phase-logic.md:371–373`

**What the LLM is currently doing:** On resume, checking for orphaned worktree branches from a previous interrupted run by listing git worktrees and matching against the epic's branch naming pattern (`epic-{N}/story-*`). Then reporting findings and asking the user whether to resume or clean up.

**What a script would do instead:** An `epic-worktrees.py` (or extend `epic-plan.py` with `--check-worktrees`) that:
1. Runs `git worktree list --porcelain`
2. Filters for branches matching `epic-{N}/story-*`
3. Cross-references with sprint-status.yaml to identify which are orphaned (story not `done`) vs. stale (story already `done`)
4. Outputs JSON: `{"orphaned": [{"branch": "epic-13/story-2", "path": "/tmp/wt-13-2", "story_status": "in-progress"}], "stale": []}`

**Estimated token savings:** ~150 tokens
**Implementation language:** Python (git subprocess + parsing)
**Pre-pass potential:** Yes — could be added to the execution plan as a resume-state field
**Standalone value:** High — useful for manual cleanup and CI health checks
**Reuse across skills:** Low — specific to epic-dev worktree naming convention

---

### Finding 6: Git Merge + Cleanup Command Sequence

**Severity:** Low
**LLM Tax:** ~120 tokens
**Affected file:** `references/phase-logic.md:285–295`

**What the LLM is currently doing:** Following a deterministic sequence for merging and cleaning up each approved story branch:
```
git checkout main
git merge --no-ff epic-{N}/story-{N.M} --no-gpg-sign
git worktree remove <worktree-path>
git branch -d epic-{N}/story-{N.M}
```

This is a fixed command sequence with variable substitution — no judgment involved.

**What a script would do instead:** A `merge-story.sh` that:
1. Accepts `--epic`, `--story`, `--worktree-path`
2. Executes the merge sequence
3. Reports success/failure and merge conflict details if any
4. Handles the error case (conflict detection) that the LLM currently needs to interpret

**Estimated token savings:** ~120 tokens
**Implementation language:** Bash
**Pre-pass potential:** No — this is an execution-time operation
**Standalone value:** Medium — reusable for manual story merges
**Reuse across skills:** Low — specific to the worktree-based workflow

---

### Finding 7: Commit Message Generation

**Severity:** Low
**LLM Tax:** ~80 tokens
**Affected files:**
- `references/phase-logic.md:119` — `chore(epic-{N}): create story specifications`
- `references/phase-logic.md:174` — `feat(epic-{N}): {story_key} — {title}`
- `references/phase-logic.md:269` — `fix(epic-{N}): {story_key} — address review findings`

**What the LLM is currently doing:** Reading three commit message templates and substituting variables. Trivially deterministic.

**What a script would do instead:** Could be folded into the prompt renderer (Finding 1) or a dedicated `commit-msg.sh`:
```bash
./commit-msg.sh --type chore --epic 13 --story-key 13-2-auth --title "Add auth middleware"
```

**Estimated token savings:** ~80 tokens (minor — three short templates)
**Implementation language:** Bash (one-liner string formatting)
**Pre-pass potential:** No — execution-time only
**Standalone value:** Low
**Reuse across skills:** Low — commit conventions are project-specific

---

### Finding 8: Customization Preamble — Existence Check + Inline Injection

**Severity:** Medium
**LLM Tax:** ~320 tokens (preamble repeated 4× across templates)
**Affected files:**
- `references/phase-logic.md:80–84` — create-story preamble
- `references/phase-logic.md:157–160` — dev-story preamble
- `references/phase-logic.md:208–210` — code-review preamble
- `references/phase-logic.md:252–254` — fix-pass preamble

**What the LLM is currently doing:** Each subagent prompt starts with the same ~80-token preamble telling the subagent to check for and load a `_bmad/custom/<skill>.toml` file. This preamble is repeated verbatim four times. The subagent then spends tokens reading (or failing to read) the file at runtime.

**What a script would do instead:** This is naturally subsumed by Finding 1 (prompt renderer). The script would:
1. Check if `_bmad/custom/<skill>.toml` exists
2. If it does, read it and inject the content directly into the rendered prompt
3. If it doesn't, omit the preamble entirely — zero wasted tokens for the subagent

**Estimated token savings:** ~320 tokens for the orchestrator (preamble text) + variable subagent savings (no file-check round-trip when file doesn't exist)
**Implementation language:** Python (part of render-prompt.py)
**Pre-pass potential:** High — preloading customization eliminates a cold-start penalty for every subagent
**Standalone value:** Medium — any orchestrator dispatching customized subagents benefits
**Reuse across skills:** High — the customization preamble pattern is used across multiple BMAD orchestrator skills

---

### Finding 9: Resume State Action Mapping

**Severity:** Low
**LLM Tax:** ~100 tokens
**Affected file:** `references/phase-logic.md:361–369`

**What the LLM is currently doing:** Reading a state→action mapping table and applying it to each story:

| Story State | Action |
|---|---|
| `backlog` | Include in Phase A |
| `ready-for-dev` | Include in Phase B from B.1 |
| `in-progress` | Include in Phase B from B.1 |
| `review` | Include in Phase B from B.2 |
| `done` | Skip |

This is a lookup table — purely deterministic.

**What a script would do instead:** Extend `epic-plan.py` to include a `required_action` field per story in its JSON output, replacing the LLM's need to interpret the mapping table. The existing script already outputs story statuses; adding the action mapping is a ~10-line change.

**Estimated token savings:** ~100 tokens (table + interpretation logic)
**Implementation language:** Python (extend `epic-plan.py`)
**Pre-pass potential:** Yes — directly enriches the execution plan
**Standalone value:** Low — tied to this skill's phase model
**Reuse across skills:** Low

---

## Aggregate Savings

| Finding | Severity | Est. Token Savings | Implementation Effort |
|---------|----------|-------------------:|----------------------|
| 1. Subagent prompt rendering | High | ~600 | New script (`render-prompt.py`) |
| 2. Review model resolution | Medium | ~200 | Extend `epic-plan.py` |
| 3. Sprint-status verification | Medium | ~250 | Extend `epic-plan.py` or new script |
| 4. Git diff command construction | Medium | ~150 | New script (`story-diff.sh`) |
| 5. Orphaned worktree detection | Medium | ~150 | Extend `epic-plan.py` |
| 6. Git merge + cleanup sequence | Low | ~120 | New script (`merge-story.sh`) |
| 7. Commit message generation | Low | ~80 | Fold into prompt renderer |
| 8. Customization preamble injection | Medium | ~320 | Fold into prompt renderer |
| 9. Resume state action mapping | Low | ~100 | Extend `epic-plan.py` (~10 lines) |
| **Total** | | **~1,970** | |

### Recommended Implementation Priority

**Phase 1 — Extend `epic-plan.py` (low effort, medium savings ~550 tokens):**
Findings 2, 5, 9 are natural extensions of the existing script. Add `--resolve-review-model`, `--check-worktrees`, and per-story `required_action` fields to the execution plan output.

**Phase 2 — New `render-prompt.py` (medium effort, high savings ~920 tokens):**
Findings 1, 7, 8 consolidate into a single prompt rendering script. This is the highest-ROI new script — it eliminates ~920 tokens of boilerplate template reading/reproduction and removes the repeated customization preamble.

**Phase 3 — Sprint-status verifier + git helpers (medium effort, medium savings ~520 tokens):**
Findings 3, 4, 6 are execution-time helpers. The sprint-status verifier (Finding 3) has the highest standalone value as it doubles as a project-level lint check.

### Notes

- The skill has already made the right architectural choice by extracting `epic-plan.py` with a clean "script-first, LLM-fallback" gate at phase-logic.md line 22. The same pattern should be extended to the other findings.
- Findings 1 and 8 have the highest reuse potential — any orchestrator skill that dispatches parameterized subagents would benefit from a shared prompt-rendering utility.
- The `--help` self-documentation opportunity is significant: if `render-prompt.py` documents its templates via `--list-templates` and `--show-template <name>`, prompts that invoke it can skip inlining the template text entirely and reference the script's help output.
