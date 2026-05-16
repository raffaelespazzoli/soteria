# Epic Dev — Phase Logic

Communicate with the user in `{communication_language}`.

**Contract anchor** (survives context compaction — re-read if uncertain):
- You are an orchestrator, not an implementer — delegate all work to subagents
- Every sub-skill (`bmad-create-story`, `bmad-dev-story`, `bmad-code-review`) runs in a **fresh subagent** with clean LLM context
- **Model routing:** `bmad-create-story` and `bmad-dev-story` → **claude-opus-4-6** (extended/high thinking). `bmad-code-review` → **gpt-5.4** (medium thinking). Apply via platform subagent model parameter (Cursor: `model` field; Claude Code: `--model`). If the preferred model isn't available, use the best alternative and note the substitution.
- Subagents run autonomously but you **relay "decision needed" questions** back to the user
- **Code review is non-interactive** — runs fully autonomously unless a genuine design or requirements decision is needed. Code-level fixes are applied without user involvement.
- **Decision escalation protocol:** When interrupting for user input, always: (1) describe the issue with full context — what happened, why it matters, what's at stake; (2) give a concrete example illustrating the problem; (3) lay out the available choices with trade-offs for each. Only then wait for user input.
- On any failure: **halt** with what failed, which story, and instructions to re-invoke
- `sprint-status.yaml` is the sole checkpoint — story statuses determine resume position
- Respect `--commit-policy` and `--skip-confirmations` args from SKILL.md

---

## Step 0: Validate Input and Parse Epic

**Goal:** Identify the target epic, verify it's actionable, and build the execution plan.

If an `epic-plan.py` script exists at `./scripts/epic-plan.py`, run it to extract the execution plan as JSON (faster, deterministic). Otherwise, perform the steps below manually.

1. If the user didn't provide an epic number, check `sprint-status.yaml` for epics in `backlog` state. If exactly one, use it. If multiple, ask the user to choose.

2. Load `sprint-status.yaml` fully. Verify the epic status is `backlog` or `in-progress`. If `done`, halt — nothing to do.

3. Find the epic source file. Search in this order:
   - `{implementation_artifacts}/{epicNum}-epic-*.md` (implementation artifact — preferred, has dependency table)
   - `{planning_artifacts}/*epic*.md` (planning artifact — search for the epic section)

4. Parse the **Stories table** from the epic file. Extract for each story:
   - Story number (e.g., `13.1`)
   - Title
   - Dependency (e.g., `None`, `13.1`, or `13.1, 13.4` for multiple)

5. Build a dependency graph (DAG) and compute a **topological sort**. Group stories into **dependency layers** — stories in the same layer have all dependencies satisfied by earlier layers.

6. Cross-reference with `sprint-status.yaml` to determine current state of each story:
   - `backlog` → needs Phase A (create-story)
   - `ready-for-dev` → needs Phase B (dev-story + code-review)
   - `in-progress` or `review` → needs Phase B continuation
   - `done` → skip

7. Present the execution plan to the user:
   - Epic name and story count
   - Dependency layers with parallelism opportunities (stories per layer)
   - Stories already completed (if resuming)
   - Estimated phases remaining

   Unless `--skip-confirmations`, ask: "Ready to proceed, or would you like to adjust anything?"

---

## Phase A: Create Story Specifications

**Goal:** Transform all `backlog` stories into comprehensive spec files via `bmad-create-story`.

**Parallelism:** Stories in the same dependency layer with all dependencies satisfied can be specced in parallel via concurrent subagents. Story specs are independent markdown files — no code conflicts.

For each dependency layer (in order):

1. Identify stories in this layer still in `backlog` state.

2. Launch subagents — one `bmad-create-story` per story, using **claude-opus-4-6** (extended/high thinking). If multiple stories are ready, launch them in parallel:

   **Subagent prompt template:**
   ```
   Run the bmad-create-story skill for story {epicNum}.{storyNum} in epic {epicNum}.
   The story title is "{title}".
   Process the story fully and autonomously — no user interaction needed.
   If you encounter a blocking issue that requires a decision, describe the
   decision needed clearly and halt.
   When complete, report: story_key, output_file_path, status (success/failed/decision_needed),
   and a one-line summary.
   ```

   **Model parameter:** Set model to `claude-opus-4-6` on each subagent (Cursor: `model` field on Task tool; Claude Code: `--model claude-opus-4-6`). If unavailable, use the best alternative and note the substitution to the user.

3. Wait for all subagents in the batch to complete.
   - If any reports `decision_needed`: relay the question to the user, get the answer, and resume that subagent.
   - If any fails: halt the epic and report which story failed and why.

4. Fall back to sequential execution if parallel subagents aren't available.

5. After each batch, verify sprint-status updates (stories → `ready-for-dev`, epic → `in-progress`).

6. Report progress: "Created specs for stories {list}. {remaining} remaining."

When all `backlog` stories have specs:

7. **Review gate** (always, even with `--skip-confirmations`): List all created story spec files with paths and ask the user to review them before development begins. This is the last chance to adjust requirements, acceptance criteria, or scope before code is written.

   ```
   Phase A complete — {count} story specs created:
   {list of story files with paths}

   Please review the story specs. When satisfied, confirm to proceed.
   (You can edit any story file now — changes will be picked up by dev-story.)
   ```

   Wait for explicit user confirmation before continuing.

8. **Commit** (respecting `--commit-policy`):
   ```
   chore(epic-{N}): create story specifications
   ```

9. Proceed to Phase B.

---

## Phase B: Implement, Review, and Commit Stories

**Goal:** For each dependency layer, develop and review stories in parallel using **git worktrees**, then merge and commit.

### Worktree-Based Parallel Execution

Stories within the same dependency layer are independent — their dependencies are all `done` from previous layers. Each story runs in its own git worktree (separate branch, separate working directory), so parallel development is safe with no working-tree conflicts.

**Subagent type:** Use `best-of-n-runner` subagents when available — they run in isolated git worktrees automatically. Fall back to sequential `generalPurpose` subagents on the main branch if worktree subagents aren't available.

For each dependency layer (in topological order):

### B.1: Launch Parallel Development

1. Identify stories in this layer where status is `ready-for-dev` (or `in-progress`/`review` for resumption).

2. For each story, launch a `best-of-n-runner` subagent. Each gets its own worktree branch (e.g., `epic-{N}/story-{N.M}`):

   **Subagent prompt template (dev + review in one worktree):**
   ```
   You are developing and reviewing story {epicNum}.{storyNum} for epic {epicNum}.
   The story file is at: {implementation_artifacts}/{story_key}.md

   Execute these steps in order:

   STEP 1 — IMPLEMENT: Run the bmad-dev-story skill.
   Process the story fully — implement all tasks, run all tests, mark complete.
   If you encounter a blocking issue requiring a human decision, describe it
   clearly and halt. Do NOT make assumptions on behalf of the user.

   STEP 2 — CODE REVIEW: Run the bmad-code-review skill on your changes.
   Run the review fully and autonomously — complete all layers, produce the
   triage report. Apply all code-level fixes without user interaction.
   Only halt for genuine design or requirements decisions that you cannot
   resolve from the story spec alone. If halting, provide: (1) full context
   on the issue and why it matters, (2) a concrete example, (3) the available
   choices with trade-offs for each.

   STEP 3 — REVIEW FIXES: If code review requested changes, re-run dev-story
   to address review findings (it supports review continuation mode), then
   re-run code review. Repeat until approved or halted.

   STEP 4 — COMMIT: When review approves, commit your changes:
   git add -A && git commit -m "feat(epic-{N}): {story_key} — {title}"

   When complete, report: story_key, status (success/failed/decision_needed),
   review_patches_count, files_changed_count, commit_sha, and a one-line summary.
   ```

   **Model parameters:**
   - STEP 1 (dev-story): use **claude-opus-4-6** with extended/high thinking
   - STEP 2 (code-review): use **gpt-5.4** with medium thinking
   - In a single worktree subagent, set the subagent's model to `claude-opus-4-6` (the primary work). For the code-review step within, spawn a nested subagent with model `gpt-5.4` if the platform supports it. If nested model switching isn't possible, use the worktree subagent's model for both and note the substitution.

3. Launch all stories in the layer simultaneously. Monitor for completion.

### B.2: Handle Subagent Results

4. As each subagent completes:
   - **Success:** Note the story's branch name and commit SHA.
   - **Decision needed:** Relay the question to the user with full context. After getting the answer, resume that subagent.
   - **Failed:** Halt the entire epic. Report which story failed, the error, and resume instructions. Other in-flight subagents for the same layer can continue to completion (fail-forward within a layer) but no new layers start.

5. Wait for all subagents in the layer to finish (or halt).

### B.3: Merge Branches

6. After all stories in the layer succeed, merge each story branch back to the main branch **sequentially** (to maintain a clean linear history):

   For each completed story branch:
   ```
   git checkout main
   git merge --no-ff epic-{N}/story-{N.M}
   ```

   If merge conflicts occur: halt with details. The user resolves conflicts manually, then re-invokes to resume. The conflicting branch is preserved for inspection.

7. Clean up worktrees after successful merge:
   ```
   git worktree remove <worktree-path>
   git branch -d epic-{N}/story-{N.M}
   ```

8. Update `sprint-status.yaml`: mark each merged story as `done`.

### B.4: Report Layer Progress

9. Report to the user:
   ```
   Layer {L} complete: {count} stories merged
   {story_list with review patch counts and file counts}
   Progress: {done}/{total} stories in epic {N}
   ```

10. Continue to the next dependency layer.

### B.5: Sequential Fallback

If `best-of-n-runner` subagents aren't available, process stories **sequentially on the main branch** within each layer:

For each story:
1. Spawn a `generalPurpose` subagent for `bmad-dev-story` with model **claude-opus-4-6** (extended/high thinking)
2. On success, spawn a `generalPurpose` subagent for `bmad-code-review` with model **gpt-5.4** (medium thinking)
   - Code review runs non-interactively — apply all code-level fixes autonomously
   - Only relay to the user for genuine design/requirements decisions, following the decision escalation protocol (context + example + choices with trade-offs)
3. If changes requested: re-run dev-story (claude-opus-4-6) then code-review (gpt-5.4) until approved
4. Commit (respecting `--commit-policy`):
   ```
   feat(epic-{N}): {story_key} — {story title}
   ```
5. Update sprint-status, report progress, continue to next story

### B.6: Check for New Stories

After all stories in the current set are done:

11. Re-read `sprint-status.yaml`. Check if any **new** `backlog` stories appeared in this epic (code review can spawn stories).

12. If new stories exist:
    - Report: "Code review spawned {count} new stories: {list}. Looping back to create specs."
    - Return to **Phase A** for the new stories, then back to Phase B.

13. If no new stories: proceed to completion.

---

## Completion

Report the epic summary:
- Total stories completed (including review-spawned)
- Total review patches across all stories
- Total commits and merge operations
- Dependency layers processed
- Any notable findings from code reviews

Update `sprint-status.yaml`: epic status → `done` (only if ALL stories are `done`).

```
Epic {N} complete: "{epic title}"
{total} stories implemented, reviewed, and committed across {layers} dependency layers.
```

---

## Resume Behavior

When invoked for an epic that's already `in-progress`, the workflow detects the current state from `sprint-status.yaml` and resumes:

| Story State | Action |
|---|---|
| `backlog` | Include in Phase A |
| `ready-for-dev` | Include in Phase B (dev + review) |
| `in-progress` | Include in Phase B (dev-story resumes from last task) |
| `review` | Include in Phase B (code-review, or dev-story if review follow-ups exist) |
| `done` | Skip |

The dependency graph is re-parsed on resume. Stories whose dependencies aren't yet `done` are deferred until their dependencies complete — the topological sort handles this naturally.

Check for orphaned worktree branches (e.g., `epic-{N}/story-*`) from a previous interrupted run. If found, report them and ask the user whether to resume from those branches or clean them up.
