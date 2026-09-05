# Execution Efficiency Analysis — `bmad-epic-dev`

**Scanner:** ExecutionEfficiencyBot  
**Skill:** `bmad-epic-dev`  
**Date:** 2026-09-05  
**Pre-pass status:** pass (0 issues — no formal stage/dependency declarations to analyze)

---

## Assessment

The `bmad-epic-dev` skill is **well-optimized for execution efficiency**. It demonstrates strong parallelization architecture — stories within dependency layers run as concurrent subagents in both Phase A (spec creation) and Phase B (implementation and review). The deterministic Python pre-pass (`epic-plan.py`) offloads DAG computation from the LLM, saving tokens and improving reliability. The primary efficiency concern is that the parent reads story specs and extracts diffs before delegating reviews, accumulating context that could instead be delegated to the review subagents themselves.

---

## Key Findings

### 1. Parent reads story specs and diffs before delegating to review subagents — Medium

**File:** `references/phase-logic.md`, lines 197–222  
**Current pattern:** In Phase B.2, the parent (a) runs `git diff main..epic-{N}/story-{N.M}` to extract the diff, (b) reads the full story spec from `{implementation_artifacts}/{story_key}.md`, and then (c) embeds both as `{story_spec_content}` and `{diff_output}` in the review subagent prompt.

```
# Current — parent reads, then delegates:
7a. Extract diff (parent runs git diff → diff in parent context)
7b. Read story spec (parent reads file → spec in parent context)  
7c. Launch review subagent with embedded diff + spec
```

**Problem:** For a dependency layer with N stories, the parent accumulates N diffs + N story specs in its context window. A code diff for a full story implementation can easily reach 500–2,000 tokens; a story spec adds another 300–800 tokens. For a layer with 5 stories, this adds 4,000–14,000 tokens to the parent — tokens the parent never uses beyond copy-pasting into subagent prompts.

**Efficient alternative:** Delegate reading to the review subagent. The review subagent can run `git diff` and `Read` the spec file itself, maintaining the same isolation guarantees since the subagent prompt still constrains what the reviewer sees:

```
# Efficient — delegate reading:
For each story, launch review subagent with:
  "Run: git diff main..epic-{N}/story-{N.M} -- . ':(exclude)_bmad-output' ':(exclude)*.md'
   Read: {implementation_artifacts}/{story_key}.md
   Then run bmad-code-review on the diff against the spec."
```

**Estimated savings:** ~1,000–3,000 tokens per story in parent context (diff + spec text), scaling with layer width.

**Trade-off note:** The current design provides a "secure courier" pattern where the parent controls exactly what the reviewer sees. Delegating reads is still isolated (the subagent only reads what it's told to), but the parent loses visibility into what was passed. For this use case — where the goal is adversarial blindness, not data restriction — delegation is safe and preferred.

---

### 2. Diff-extraction and spec-reading described sequentially per story before batch review launch — Low

**File:** `references/phase-logic.md`, lines 196–234  
**Current pattern:** Steps 7a–7c describe a per-story sequential flow (extract diff → read spec → launch subagent), then step 8 says "launch all reviews in the layer simultaneously." The intended behavior is batch launch, but the per-story sequential description in 7a–7c may cause an LLM orchestrator to process stories one-by-one (gathering diff + spec for story 1, then story 2, etc.) before launching all reviews.

**Efficient alternative:** Restructure the instruction to make batching explicit:

```
7. For ALL stories in the layer simultaneously:
   a. Extract the diff (can run all git-diff commands in parallel)
   b. Read the story spec
   c. Launch review subagent with diff + spec

   Launch all review subagents in a single message batch.
```

Or, if finding #1 is adopted (delegate reading), this becomes moot — launch all review subagents in parallel with instructions to self-read.

**Estimated savings:** One round-trip per additional story in the layer (~1–2s each). Minor for small layers, noticeable for layers with 4+ stories.

---

### 3. Config files loaded without explicit parallel instruction — Low

**File:** `SKILL.md`, lines 33–39  
**Current pattern:** "Load available config from `{project-root}/_bmad/config.yaml` and `{project-root}/_bmad/config.user.yaml`" — a single instruction covering two file reads. An LLM may naturally batch these, but the instruction doesn't explicitly call for parallel reads.

**Efficient alternative:** Minor — add "(read both in a single message)" or restructure as a bullet list of files to batch-read. These are small files, so the impact is minimal.

**Estimated savings:** ~1 round-trip (~1–2s). Negligible in the context of a full epic orchestration.

---

### 4. Subagent return format is descriptive but not JSON-enforced — Low

**Files:** `references/phase-logic.md`, lines 86–92 (Phase A subagent), lines 166–177 (Phase B.1 subagent), lines 226–232 (Phase B.2 subagent)  
**Current pattern:** Subagent prompts specify return fields ("Report: story_key, status, files_changed_count...") but use natural-language formatting rather than strict JSON schema with "ONLY return" constraint language.

**Example (current):**
```
When complete, report: story_key, output_file_path, status (success/failed/decision_needed),
and a one-line summary.
```

**Efficient alternative:**
```
When complete, return ONLY a JSON object — no other output:
{
  "story_key": "...",
  "output_file_path": "...",
  "status": "success|failed|decision_needed",
  "summary": "one-line summary"
}
```

**Estimated savings:** 50–200 tokens per subagent response (eliminates conversational filler). For an epic with 10 stories × 2+ subagent calls each, this saves ~1,000–4,000 tokens of parent context from result accumulation.

---

## Optimization Opportunities

### Structural: Delegate all reads to review subagents (addresses Findings #1 and #2)

Refactor Phase B.2 so the parent launches review subagents with file paths and git commands instead of pre-read content. The subagent runs the diff and reads the spec itself. This:
- Eliminates parent context growth proportional to layer width × (diff + spec size)
- Makes batch launching trivial (no pre-gathering step)
- Maintains adversarial isolation (subagent still only sees diff + spec)
- **Estimated impact:** Saves 4,000–14,000 tokens of parent context per dependency layer, enables cleaner parallel launch

### Tactical: JSON-enforce all subagent return formats (addresses Finding #4)

Standardize all three subagent prompt templates (Phase A, B.1, B.2) to require JSON-only output with explicit schema. This improves parseability and reduces parent context from verbose responses.
- **Estimated impact:** Saves ~1,000–4,000 tokens across a full epic run, improves reliability of result parsing

---

## What's Already Efficient

1. **Dependency-layer parallelism** — Both Phase A and Phase B parallelize stories within dependency layers using concurrent subagents. This is the highest-impact optimization and it's done correctly. (`phase-logic.md`, lines 68–70, 143–148)

2. **Deterministic Python pre-pass** — `epic-plan.py` offloads DAG computation (topological sort, status cross-referencing, layer grouping) from the LLM to a deterministic script. This saves hundreds of tokens of LLM parsing and eliminates non-determinism in dependency resolution. (`phase-logic.md`, line 22; `scripts/epic-plan.py`)

3. **Worktree-based isolation** — `best-of-n-runner` subagents run in isolated git worktrees, enabling true parallel development with no working-tree conflicts. Graceful fallback to sequential execution when worktrees aren't available. (`phase-logic.md`, lines 143–147, 314–326)

4. **Strict orchestrator-not-implementer contract** — The parent never writes code, specs, or reviews. All work is delegated to specialized sub-skills in fresh subagent contexts. This keeps parent context lean and focused on coordination. (`SKILL.md`, line 12; `phase-logic.md`, lines 3–4)

5. **Fail-fast validation** — Step 0 validates epic status, locates source files, and verifies actionability before any expensive operations. A `done` epic exits immediately. (`phase-logic.md`, lines 18–60)

6. **Review gate between phases** — Phase A ends with a mandatory user review gate before Phase B begins. This prevents wasted implementation effort on incorrect specs. (`phase-logic.md`, lines 106–116)

7. **Checkpoint-based resume** — `sprint-status.yaml` serves as a persistent checkpoint. Re-invocation skips completed work entirely, picking up at the exact story and phase that was interrupted. (`phase-logic.md`, lines 358–373)

8. **Adversarial review architecture** — Separate model, separate subagent, zero dev context. The review model is resolved once in Step 0 and reused. Fresh context per review ensures truly blind adversarial analysis. (`phase-logic.md`, lines 131–139)

9. **Bounded review cycles** — Max 2 review rounds per story before escalating to the user. Prevents infinite fix/review loops. (`phase-logic.md`, line 245)

10. **Review-spawned story detection** — Phase B.7 checks for new stories added by code review, looping back to Phase A only when needed. Avoids manual re-invocation. (`phase-logic.md`, lines 328–337)
