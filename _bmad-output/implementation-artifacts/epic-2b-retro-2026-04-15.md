# Epic 2b Retrospective: VM-to-DRPlan Label Convention Refactoring

**Date:** 2026-04-15
**Facilitator:** Bob (Scrum Master)
**Participants:** Alice (Product Owner), Charlie (Senior Dev), Dana (QA Engineer), Elena (Junior Dev), Amelia (Developer), Winston (Architect), Raffa (Project Lead)

---

## Epic Summary

| Metric | Value |
|--------|-------|
| Stories completed | 5/5 (100%) |
| Stories | 2b.1, 2b.1.5, 2b.2, 2b.3, 2b.4 |
| Originally planned stories | 3 (grew to 4 in Epic 2 retro, then 5 during execution) |
| Organically spawned stories | 1 (Story 2b.1.5 — Cluster-Scoped CRD Migration) |
| Code review patches applied | 15+ across stories |
| Net code change | Significant deletion — ExclusivityChecker (453 lines), DRPlanValidator shrunk 66%, mapVMToDRPlans O(N)→O(1) |
| Production incidents | 0 |
| Epic 1 debt closed | 1 item (scan cap test — promoted to Story 2b.4) |
| Pre-existing debt carried | RBAC failures (3 tests), lint warnings (6), OLM bundle validation |

---

## Successes

1. **Design simplification achieved.** Structural exclusivity via Kubernetes label semantics replaced O(plans × VMs) code-enforced checks. `ExclusivityChecker` (453 lines) deleted entirely. `DRPlanValidator` shrank from ~254 to ~87 lines. `mapVMToDRPlans` went from O(N) DRPlan scanning to O(1) label read.

2. **No meaningful flexibility lost.** The bottom-up "VM opts into a plan" model (`soteria.io/drplan: <planName>` label) is more natural than the top-down "plan claims VMs" model (`vmSelector`). A Kubernetes label key can only have one value per resource, making exclusivity a structural invariant. The team consensus: the old flexibility was accidental complexity.

3. **Cluster-scoped migration resolved two coupled defects.** Story 2b.1's code review caught that namespace-scoped DRPlans were fundamentally incompatible with cross-namespace VM management: (a) `enqueueForVM()` targeted wrong namespace, (b) same-name plans in different namespaces collided on label value. Story 2b.1.5 made all 3 CRDs cluster-scoped, aligning resource identity with operational scope. ScyllaDB storage needed zero changes.

4. **Code reviews caught real defects in every story.** 2b.1: cross-namespace design flaw. 2b.1.5: missing registry strategy unit tests. 2b.2: doc accuracy, integration test verification. 2b.3: six stale references across PRD/architecture/NFR docs. 2b.4: missing no-overlap page assertions.

5. **Epic 2 retro action items followed through.** Task checkboxes maintained in all story files. Tiered documentation standards applied consistently. Scan cap test debt promoted to Story 2b.4 and closed.

6. **Story sequencing was deliberate and effective.** Risk front-loaded (2b.1 core refactoring), design correction applied early (2b.1.5), simplification capitalized on (2b.2), documentation batched into dedicated sweep (2b.3), old debt closed last (2b.4).

---

## Challenges

1. **Scope grew from 3 planned stories to 5.** Story 2b.1.5 spawned from code review findings. Story 2b.4 was added during Epic 2 retro. Team consensus: acceptable for alpha-stage project, but rate of organic spawning should decrease as architecture matures.

2. **Pre-existing debt carried without resolution for two epics.** 3 RBAC integration test failures (from Story 2.5), 6 lint warnings (in untouched files), and OLM bundle validation (from Epic 1) were noted as "pre-existing" in every story but never fixed. Decision made in this retro: fix RBAC and lint before Epic 3, park OLM until later.

3. **Documentation sweep story needed review to catch completeness gaps.** Story 2b.3's code review found 6 additional stale references the initial implementation missed (old label names in PRD narrative, architecture naming table mismatch, NFR scale row wording). Mitigation: future sweep stories should include an explicit search-and-replace term checklist.

---

## Key Insights

1. **Kubernetes label semantics can replace code-enforced invariants.** A label key having exactly one value per resource makes exclusivity structural. This insight eliminates O(plans × VMs) admission checks and simplifies the entire validation model.

2. **Refactoring epics benefit from batching documentation updates.** Doing all code changes first (2b.1, 2b.1.5, 2b.2), then sweeping documentation in a dedicated story (2b.3) avoids wasted rework when design evolves during implementation.

3. **Organic story spawning from code review is healthy in alpha.** The pattern (2.3.1 in Epic 2, 2b.1.5 in Epic 2b) catches design issues early when the cost of fixing them is low. Should decrease as architecture stabilizes — track across epics.

4. **Cluster scope aligns resource identity with operational scope.** DRPlan, DRExecution, and DRGroupStatus are operational platform resources, not tenant artifacts. Making them cluster-scoped resolved namespace ambiguity and simplified cross-namespace VM management.

---

## Epic 2 Retrospective Follow-Through

| # | Action Item | Status | Evidence |
|---|-------------|--------|----------|
| 1 | Maintain task-level checkboxes in story files | ✅ Completed | All 5 story files have properly checked task boxes |
| 2 | Continue tiered documentation standards | ✅ Completed | Stories 2b.1, 2b.1.5, 2b.2 updated Tier 2/3 comments; 2b.2 updated doc.go |
| 3 | Story 1.3.1 scan cap test (Low) | ✅ Resolved | Promoted to Story 2b.4 — integration tests written and passing |
| 4 | Story 1.7 OLM bundle validation (Low) | ❌ Not addressed | Parked — revisit after core epics, per Raffa |
| 5 | Story 2.5: credential sanitization wiring | ⏳ Blocked | Unblocks with Epic 3 driver integration |
| 6 | Story 2.5: sanitization output tests | ⏳ Blocked | Same blocker as #5 |
| 7 | Story 2.5: RBAC integration tests | ⏳ Blocked | E2E infrastructure not yet built |
| 8 | Story 2.4: static StorageClassDriverMap | ⏳ Blocked | Resolves with Epic 3 Story 3.1 |
| 9 | Story 2.3: webhook in-process admission fallback | ✅ Closed | Switching to `matchPolicy: Exact` eliminates the risk |

**Summary:** 4 items completed/resolved, 1 closed (no longer needed), 3 blocked on downstream epics, 1 parked.

---

## Action Items

### Process Improvements

| # | Action | Owner | Success Criteria |
|---|--------|-------|-----------------|
| 1 | Continue task-level checkbox maintenance in story files | All devs | No story reaches "done" with unchecked boxes |
| 2 | Continue tiered documentation standards | All devs | Every Epic 3 story ships with Tier 1-3 comments |
| 3 | Include systematic search-and-replace checklist in documentation sweep stories | Bob (SM) | Sweep stories list explicit terms to grep for |
| 4 | Track organic story spawning rate across epics | Bob (SM) | Log spawned-story count in each retro; flag if rate doesn't decrease by Epic 5 |

### Technical Debt

| # | Item | Priority | Decision |
|---|------|----------|----------|
| 5 | RBAC integration test failures (3 tests) | High | **Fix before Epic 3** |
| 6 | Lint warnings (6 issues) in untouched files | High | **Fix before Epic 3** |
| 7 | OLM bundle validation (from Epic 1) | Low | **Parked** — revisit after core epics |
| 8 | Story 2.5: credential sanitization wiring + output tests | Blocked | Unblocks with Epic 3 driver integration |
| 9 | Story 2.5: RBAC integration tests against aggregated API | Blocked | Unblocks with e2e test infrastructure |
| 10 | Story 2.3: webhook in-process admission fallback | — | **Closed** — switching to `matchPolicy: Exact` eliminates the risk |

### Team Agreements

- All stories continue through `bmad-dev-story` workflow — no exceptions
- Code reviews continue using a different LLM than the implementing agent
- Task checkboxes in story files are updated as work progresses
- Deferred items reviewed at start of each epic
- Pre-existing debt gets an explicit decision each retro: fix, park with rationale, or close as won't-fix

### Design Decisions Made

- **Webhook `matchPolicy`:** Change from `Equivalent` to `Exact` on both handlers. Aggregated API server controls exact group/version/resource — no version aliasing. Eliminates deployment risk.
- **Driver registration pattern:** Use central `pkg/drivers/all` import package for `init()`-based registration. Keeps `main.go` clean. Per-package alternative documented for future reference if subset builds needed.

---

## Epic 3 Preparation

### Preparation Tasks

| Status | Task | Owner |
|--------|------|-------|
| [ ] | Fix 3 RBAC integration test failures (create ClusterRole manifests for cluster-scoped CRDs) | Charlie |
| [ ] | Fix 6 pre-existing lint warnings in untouched files | Amelia |
| [ ] | Change webhook `matchPolicy` from `Equivalent` to `Exact` on both handlers, run `make manifests` | Charlie |
| [ ] | Story 3.1 file should specify `drivers/all` central import pattern for driver registration | Bob (SM) |

### Dependencies on Epic 2/2b

- DRPlan controller and preflight pipeline are stable — no changes needed for Epic 3
- Story 2.4's static `StorageClassDriverMap` replaced by dynamic driver registry in Story 3.1
- Story 2.5's deferred credential sanitization unblocks when drivers make real API calls

### No Critical Blockers (after prep tasks)

All Epic 2b code is complete and tested. Prep tasks address carried debt and a design decision.

---

## Epic 3 Preview

**Epic 3: Storage Driver Framework & Reference Implementations** (4 stories, backlog)

- 3.1: StorageProvider Interface & Driver Registry
- 3.2: No-Op Driver
- 3.3: Fake Driver for Unit Testing
- 3.4: Conformance Test Suite

**Nature:** Greenfield — new `pkg/drivers/` package, isolated from Epic 2/2b refactoring. Different risk profile than refactoring epics.

---

## Readiness Assessment

| Area | Status |
|------|--------|
| Testing & Quality | ✅ All unit and integration tests passing (except pre-existing RBAC — fixing in prep) |
| Codebase Health | ✅ Net code deletion. Clean label-based model. Tiered docs applied |
| User Validation | ⚠️ Not performed this epic |
| Deployment | ✅ `matchPolicy: Exact` change resolves webhook deployment risk |
| Technical Health | ✅ Stable. O(1) label-based discovery. Cluster-scoped CRDs aligned with operational scope |
| Unresolved Blockers | ✅ None after prep tasks |

---

## Next Steps

1. Execute preparation tasks (RBAC fix, lint cleanup, matchPolicy change)
2. Begin Epic 3 story creation with `create-story`
3. Review deferred items at Epic 3 kickoff
4. After Epic 3: proceed to Epic 4 (DR Workflow Engine)
