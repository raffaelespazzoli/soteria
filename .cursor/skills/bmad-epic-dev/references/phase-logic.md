# Epic Dev — Phase Logic

Communicate with the user in `{communication_language}`.

**Contract anchor** (survives context compaction — re-read if uncertain):
- You are an orchestrator, not an implementer — delegate all work to subagents
- Every sub-skill (`bmad-create-story`, `bmad-dev-story`, `bmad-code-review`) runs in a **fresh subagent** with clean LLM context
- **Dev and review are SEPARATE subagents** — NEVER combine them. The reviewer must have ZERO dev context.
- **Code review uses a DIFFERENT model** (`{review_model}`) — NEVER the same model that wrote the code.
- **All subagent prompts include the customization preamble** — load `{project-root}/_bmad/custom/<skill>.toml` before executing
- Subagents run autonomously but you **relay "decision needed" questions** back to the user
- On any failure: **halt** with what failed, which story, and instructions to re-invoke
- `sprint-status.yaml` is the sole checkpoint — story statuses determine resume position
- `--commit-policy`: `auto` (commit at prescribed points), `ask` (prompt before each), `skip` (user handles commits)
- `--skip-confirmations`: suppress soft-gate pauses between phases (does NOT override the mandatory Phase A review gate in interactive mode)
- `--review-model`: override the auto-selected adversarial review model
- `--headless`: full automation — auto-approve specs, fail-fast on decisions, auto-clean worktrees, JSON exit summary. Implies `--commit-policy=auto --skip-confirmations`

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

5. Build a dependency graph (DAG) and compute a **topological sort**. Group stories into **dependency layers** — stories in the same layer have all dependencies satisfied by earlier layers. If circular dependencies are detected (stories that can't be placed in any layer), **warn the user explicitly** — "Circular dependency detected: {story_list}. These stories cannot proceed until the cycle is resolved." Do not silently absorb them into an orphan layer.

6. Cross-reference with `sprint-status.yaml` to determine current state of each story:
   - `backlog` → needs Phase A (create-story)
   - `ready-for-dev` → needs Phase B (dev-story + code-review)
   - `in-progress` or `review` → needs Phase B continuation
   - `done` → skip
   - **Any other status (or missing from sprint-status):** Warn the user — "Story {key} has unexpected status '{status}'. Treating as backlog." Include in Phase A unless the user overrides.

7. **Resolve review model** for adversarial code review (Phase B.2):
   - If `--review-model` arg was provided → use it
   - Else if `{project-root}/_bmad/config.user.yaml` has `bmm.review_model` → use it
   - Else → auto-select a model different from the current session's model. Available options (pick one that differs from the dev model; **maintain this list** when the platform's model roster changes):
     - `cursor-grok-4.6-xhigh`
     - `claude-4.6-opus-high-thinking`
     - `gpt-5.4-medium`
     - `composer-2.5`
   - If no alternative is available → warn the user that same-model review reduces adversarial value, proceed with same model but enforce fresh context (separate subagent, no dev history)

8. Present the execution plan to the user:
   - Epic name and story count
   - Dependency layers with parallelism opportunities (stories per layer)
   - Stories already completed (if resuming)
   - Review model that will be used (and why)
   - Estimated phases remaining

   Unless `--skip-confirmations` or `--headless`, ask: "Ready to proceed, or would you like to adjust anything?"
   In `--headless` mode: log the plan summary and proceed immediately.

---

## Phase A: Create Story Specifications

**Goal:** Transform all `backlog` stories into comprehensive spec files via `bmad-create-story`.

**Parallelism:** Stories in the same dependency layer with all dependencies satisfied can be specced in parallel via concurrent subagents. Story specs are independent markdown files — no code conflicts.

For each dependency layer (in order):

1. Identify stories in this layer still in `backlog` state.

2. Launch subagents — one `bmad-create-story` per story. If multiple stories are ready, launch them in parallel:

   **Subagent prompt:** Generate via `./scripts/render-prompt.py create-story --project-root={project-root} --epic-num={epicNum} --story-num={storyNum} --title="{title}"` and use the output as the subagent prompt.

3. Wait for all subagents in the batch to complete.
   - If any reports `decision_needed`: relay the question to the user, get the answer, and resume that subagent.
   - If any fails: halt the epic and report which story failed and why.

4. Fall back to sequential execution if parallel subagents aren't available.

5. After each batch, verify sprint-status updates (stories → `ready-for-dev`, epic → `in-progress`).

6. Report progress: "Created specs for stories {list}. {remaining} remaining."

When all `backlog` stories have specs:

7. **Review gate:**

   **Interactive mode** (always, even with `--skip-confirmations`): List all created story spec files with paths and ask the user to review them before development begins. This is the last chance to adjust requirements, acceptance criteria, or scope before code is written.

   ```
   Phase A complete — {count} story specs created:
   {list of story files with paths}

   Please review the story specs. Key things to verify:
   - Acceptance criteria are testable and unambiguous
   - Dependencies between stories are correct
   - Scope boundaries (Anti-Patterns / DO NOT sections) are clear
   - No missing stories or gaps in the epic's requirements

   When satisfied, confirm to proceed.
   (You can edit any story file now — changes will be picked up by dev-story.)
   ```

   Wait for explicit user confirmation before continuing.

   **Headless mode** (`--headless`): Log a warning — "⚠️ Headless: auto-approving {count} story specs (review gate bypassed). Specs: {list of paths}" — and proceed immediately. The specs are still written to disk for post-run review.

8. **Commit** (respecting `--commit-policy`):
   ```
   chore(epic-{N}): create story specifications
   ```

9. Proceed to Phase B.

---

## Phase B: Implement, Review, and Commit Stories

**Goal:** For each dependency layer, develop stories in parallel using **git worktrees**, then run **adversarial code review with a different LLM**, fix findings, and merge.

### Critical Contract — Adversarial Review Isolation

> **Dev and review MUST run in separate subagents.** The reviewer must have ZERO context
> from the developer's session — no reasoning, no struggles, no intermediate states.
> The reviewer receives ONLY the diff and story spec, making it truly blind and adversarial.
>
> **The review model MUST differ from the dev model.** This prevents self-review blind spots
> where the same model approves patterns it would naturally generate. Use `{review_model}`
> resolved in Step 0.

### Worktree-Based Parallel Execution

Stories within the same dependency layer are independent — their dependencies are all `done` from previous layers. Each story runs in its own git worktree (separate branch, separate working directory), so parallel development is safe with no working-tree conflicts.

**Subagent type:** Use `best-of-n-runner` subagents when available — they run in isolated git worktrees automatically. Fall back to sequential `generalPurpose` subagents on the main branch if worktree subagents aren't available.

For each dependency layer (in topological order):

### B.1: Launch Parallel Development (Dev Only)

1. Identify stories in this layer where status is `ready-for-dev` (or `in-progress` for resumption).

2. For each story, launch a `best-of-n-runner` subagent. Each gets its own worktree branch (e.g., `epic-{N}/story-{N.M}`):

   **Subagent prompt:** Generate via `./scripts/render-prompt.py dev-story --project-root={project-root} --epic-num={epicNum} --story-num={storyNum} --story-key={story_key} --title="{title}" --implementation-artifacts={implementation_artifacts}` and use the output as the subagent prompt.

3. Launch all stories in the layer simultaneously. Monitor for completion. If a worktree subagent fails to start (branch already exists, locked `.git/worktrees`, disk space), report the failure and suggest: check for orphaned worktrees from a previous run, or use `git worktree prune` to clean stale entries.

4. As each dev subagent completes:
   - **Success:** Note the story's worktree branch name and commit SHA. **Do NOT merge yet** — review must pass first.
   - **Decision needed:** In interactive mode, relay the question to the user with full context. After getting the answer, resume that subagent. In `--headless` mode, halt immediately — "Decision needed for story {key}: {description}. Cannot proceed in headless mode."
   - **Failed:** Halt the entire epic. Report which story failed, the error, and resume instructions. Other in-flight subagents for the same layer can continue to completion (fail-forward within a layer) but no new layers start.

5. Wait for all dev subagents in the layer to finish (or halt).

6. Update `sprint-status.yaml`: mark each completed dev story as `review` (not `done` — review hasn't happened yet).

### B.2: Adversarial Code Review (Different Model, Fresh Context)

**Contract:** Reviews run in `generalPurpose` subagents with model `{review_model}`. The reviewer receives ONLY the diff and story spec — never the developer's reasoning or session history.

7. For each successfully developed story in this layer:

   a. Extract the diff from the worktree branch to a temp file:
      ```
      git diff main..epic-{N}/story-{N.M} -- . ':(exclude)_bmad-output' ':(exclude)*.md' > /tmp/epic-{N}-{story_key}.diff
      ```

   b. Launch a `generalPurpose` subagent with model `{review_model}`:

      **Subagent prompt:** Generate via `./scripts/render-prompt.py code-review --project-root={project-root} --story-key={story_key} --spec-file={implementation_artifacts}/{story_key}.md --diff-file={diff_file}` and use the output as the subagent prompt. (The script reads spec and diff files directly, keeping them out of the orchestrator's context.)

8. Launch all reviews in the layer simultaneously (they're independent — each reviews a different story's diff).

9. Collect results:
   - **APPROVED:** Mark story as review-passed. Ready to merge in B.4.
   - **CHANGES_REQUESTED:** Proceed to B.3 (fix pass) for this story.
   - **Decision needed:** Relay to user with full context.

### B.3: Review Fix Pass (Only If Changes Requested)

**Max cycles:** 2 review rounds per story. If still CHANGES_REQUESTED after 2 cycles, halt and escalate to user — this likely indicates a design disagreement that needs human judgment.

10. For each story with CHANGES_REQUESTED:

    a. Launch a `best-of-n-runner` subagent on the **same worktree branch** (or `generalPurpose` if worktrees unavailable):

       **Subagent prompt:** Write the review findings to a temp file, then generate via `./scripts/render-prompt.py fix-pass --project-root={project-root} --epic-num={epicNum} --story-key={story_key} --review-findings="$(cat /tmp/epic-{N}-{story_key}-findings.txt)"` and use the output as the subagent prompt.

    b. After fixes complete, re-run B.2 for this story only (extract updated diff, launch fresh review subagent with `{review_model}`).

    c. If approved → proceed to B.4. If still CHANGES_REQUESTED after 2 total review cycles → halt and escalate.

### B.4: Merge Approved Stories

11. After all stories in the layer are review-approved, merge each story branch back to the main branch **sequentially** (to maintain a clean linear history):

    For each approved story branch:
    ```
    git checkout main
    git merge --no-ff epic-{N}/story-{N.M} --no-gpg-sign
    ```

    If merge conflicts occur:
    - **Do NOT abort the merge.** Leave the conflict state intact for the user.
    - Report clearly: which story branch conflicted, which files have conflicts, and how many stories in this layer were already merged successfully.
    - Provide recovery instructions:
      ```
      Merge conflict in story {story_key}. To resolve:
      1. Resolve conflicts in the listed files
      2. git add <resolved-files> && git merge --continue --no-gpg-sign
      3. Re-invoke bmad-epic-dev for epic {N} to resume
         (sprint-status will show this story as 'review' — it will be merged on resume)
      ```
    - If this is story 2+ in the layer (partial merge), note which stories are already merged. Do NOT attempt to revert successful merges.
    - Mark the conflicting story as `merge-conflict` in `sprint-status.yaml` and halt.

12. Clean up worktrees after successful merge:
    ```
    git worktree remove <worktree-path>
    git branch -d epic-{N}/story-{N.M}
    ```

13. Update `sprint-status.yaml`: mark each merged story as `done`.

### B.5: Report Layer Progress

14. Report to the user:
    ```
    Layer {L} complete: {count} stories merged
    {story_list with review verdicts, patch counts, and file counts}
    Review model used: {review_model}
    Progress: {done}/{total} stories in epic {N}
    ```

15. Continue to the next dependency layer.

### B.6: Sequential Fallback (Alternative to B.1–B.5)

> **This section is an alternative execution mode**, not a step that runs after B.5.
> Use B.6 ONLY when `best-of-n-runner` subagents aren't available. Otherwise, use B.1–B.5 above.

If worktree subagents aren't available, process stories **sequentially on the main branch** within each layer. The dev/review separation is still mandatory:

For each story:
1. Generate dev prompt via `./scripts/render-prompt.py dev-story ...` and spawn a `generalPurpose` subagent
2. On success, extract the diff to a temp file (`git diff HEAD~1..HEAD -- . ':(exclude)_bmad-output' ':(exclude)*.md' > /tmp/epic-{N}-{story_key}.diff`)
3. Generate review prompt via `./scripts/render-prompt.py code-review --spec-file=... --diff-file=...` and spawn a **separate** `generalPurpose` subagent with model `{review_model}` — the script reads files directly, keeping them out of the orchestrator's context
4. If changes requested: generate fix-pass prompt via `./scripts/render-prompt.py fix-pass ...`, re-run, then re-review (max 2 cycles)
5. Commit (respecting `--commit-policy`):
   ```
   feat(epic-{N}): {story_key} — {story title}
   ```
6. Update sprint-status, report progress, continue to next story

### B.7: Check for New Stories

**Max spawn iterations:** 2. If code review keeps spawning new stories beyond 2 cycles (Phase A → Phase B → B.7 → Phase A), halt and escalate — this likely indicates a systemic issue that needs human judgment, not more automated iterations.

After all stories in the current set are done:

16. Re-read `sprint-status.yaml`. Check if any **new** `backlog` stories appeared in this epic (code review can spawn stories).

17. If new stories exist:
    - If this is spawn iteration 3+: halt — "Review-spawned story loop exceeded max iterations (2). {count} new stories remain: {list}. Manual intervention needed."
    - Otherwise: Report "Code review spawned {count} new stories: {list}. Looping back to create specs (iteration {n}/2)."
    - Return to **Phase A** for the new stories, then back to Phase B.

18. If no new stories: proceed to completion.

---

## Completion

Report the epic summary:
- Total stories completed (including review-spawned)
- Total review patches across all stories
- Review model used (`{review_model}`) and review cycles per story
- Total commits and merge operations
- Dependency layers processed
- Any notable findings from code reviews

Update `sprint-status.yaml`: epic status → `done` (only if ALL stories are `done`).

**Interactive mode:**
```
Epic {N} complete: "{epic title}"
{total} stories implemented, reviewed, and committed across {layers} dependency layers.
```

**Headless mode (`--headless`)** — also write a structured JSON summary to `{implementation_artifacts}/epic-{N}-summary.json`:
```json
{
  "epic": N,
  "title": "{epic title}",
  "status": "done|halted",
  "halt_reason": null,
  "stories": [
    {
      "key": "{story_key}",
      "status": "done|halted|merge-conflict",
      "review_verdict": "APPROVED|CHANGES_REQUESTED",
      "review_model": "{review_model}",
      "review_cycles": 1,
      "commit_sha": "{sha}",
      "files_changed": 0
    }
  ],
  "summary": {
    "total_stories": 0,
    "completed": 0,
    "review_patches": 0,
    "dependency_layers": 0,
    "spawn_iterations": 0
  }
}
```
This enables CI pipelines to parse results programmatically.

---

## Resume Behavior

When invoked for an epic that's already `in-progress`, the workflow detects the current state from `sprint-status.yaml` and resumes:

| Story State | Action |
|---|---|
| `backlog` | Include in Phase A |
| `ready-for-dev` | Include in Phase B from B.1 (dev + review) |
| `in-progress` | Include in Phase B from B.1 (dev-story resumes from last task) |
| `review` | Include in Phase B from B.2 (dev complete — run adversarial review with `{review_model}`) |
| `merge-conflict` | Include in Phase B from B.4 (user resolved conflict — attempt merge again) |
| `done` | Skip |

The dependency graph is re-parsed on resume. Stories whose dependencies aren't yet `done` are deferred until their dependencies complete — the topological sort handles this naturally.

Check for orphaned worktree branches (e.g., `epic-{N}/story-*`) from a previous interrupted run. If found:
- **Interactive mode:** Report them and ask the user whether to resume from those branches or clean them up.
- **Headless mode (`--headless`):** Auto-clean orphaned worktrees (remove worktree, delete branch) and log: "Headless: cleaned {count} orphaned worktrees: {list}."
