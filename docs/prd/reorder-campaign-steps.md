# PRD: Reorder Campaign Sequence Steps

**Status:** Draft for review
**Author:** Tiel-Coder (Ornith-1.5-35B-A3B)
**Date:** 2026-11-20
**Repo:** warmbly (positive-mail-49447978) · branch target: `development` (per TheDevsGarden rule)

---

## 1. Problem

A campaign is a sequence of steps. Routing follows **connections only** — position is a
purely cosmetic field that orders the canvas and picks the entry step (lowest position).
Today there is **no way to change a step's position**:

- `POST /campaigns/:id/steps` always appends at `MAX(position)+1`.
- `PATCH /campaigns/:id/steps/:sid` updates content only (name/subject/body/wait_after/thread_reply/conditions/kind/action).
- `PATCH /campaigns/:id/step-layout` updates only `x`/`y` canvas coordinates — explicitly *cosmetic and unaudited*, never touches `position`.
- `CampaignFlow.tsx` has no reorder UI.

Consequences:

- You cannot reorder a linear sequence — new steps always append at the end.
- You cannot reorder a branching sequence — the order is fixed by branch connections.
- The entry step is locked to the first-created step (lowest position).

The user's need: **rearrange email sequence order within a campaign.**

---

## 2. Why position is safe to reorder (design constraint)

Position is **not** a routing input. Routing is:

1. conditional branches (first match wins),
2. explicit `else` catch-all,
3. linear `position+1` **only when a step defines no branches**.

So position's only real jobs are:

- pick the **entry step** (lowest position),
- **order the canvas** (`GetSequences` returns `ORDER BY position ASC, created_at ASC`; frontend treats `steps[0]` as entry).

Reordering therefore **cannot change routing behavior** for any step that has branches.
For a linear step (no branches), position+1 is the next step — so reordering a linear
sequence *does* change which step follows which. That is exactly the behavior the user
wants, and it is the only place reordering has a routing effect. This is the one thing
the implementation must make explicit and test.

**Invariant to preserve:** after any reorder, `position` must remain a **strict, gap-free,
contiguous** sequence of integers starting at 1, with no duplicates. This is what the
scheduler's linear `position+1` walk and the entry-step selection rely on.

---

## 3. Goals

- G3 (must): Reorder steps within a single campaign via a dedicated API endpoint.
- G2 (should): Reorder UI in `CampaignFlow.tsx` (drag or up/down buttons).
- G1 (should): Reorder UI in the sequence template builder (`web/src/components/app/flow/`).
- G4 (must): Preserve all existing invariants (routing, entry step, canvas order, audit, permissions, org scope).

---

## 4. Non-Goals

- No change to routing logic, branch evaluation, or the scheduler.
- No cross-campaign step movement.
- No change to `step-layout` (x/y) semantics.
- No new database columns or migrations (position already exists).

---

## 5. Backend Design

### 5.1 New endpoint

`PATCH /campaigns/:id/steps/reorder`

- Body: `{ "stepIds": ["sid-1", "sid-2", ...] }` — the full ordered list of step IDs in the
  desired new order. Full-list is chosen over pairwise moves because it is atomic,
  idempotent, and lets the frontend send the exact canvas order after a drag.
- Auth: `PermManageCampaigns` (JWT) + `APIPermWriteCampaigns` (API key), same as other
  step endpoints. Tenant = organization (via `middleware.GetOrganizationID`).
- Validation:
  - All `stepIds` belong to the campaign (same `campaign_id`), else `404 not_found`.
  - No duplicates, none missing, no unknown IDs — else `400 invalid_steps`.
  - (Optional) reject reordering that would change the entry step if we decide the entry
    must stay fixed — **see open question O1**. Default: allow it (reordering includes
    choosing the entry).

### 5.2 Service method

`service.ReorderSteps(ctx, campaignID, stepIDs []string) error`

- Loads all steps for the campaign (org-scoped).
- Validates the requested order against the loaded set.
- Writes `position = i+1` for each step in the requested order, in **one transaction**.
- Audits via `AuditService.LogAction` (entity `campaign_step` or `campaign_sequence`).
- Returns `404 not_found` if the campaign doesn't exist / isn't org-owned.

### 5.3 Repository

`pg_sequence.go`: add `ReorderSteps(ctx, campaignID, stepIDs) error` (transactional batch
update of `position`). Reuse the existing `sequenceSelect`/`stepSelect` org-scoped queries.

### 5.4 Routes

Add to the sequences group in `internal/api/routes.go` (after line 986, before DELETE):

```go
sequences.PATCH("/reorder", h.ReorderSteps)
```

Param name stays `:id` (campaign) — no collision with `:sid` (the reorder path is
`/campaigns/:id/steps/reorder`, a distinct route).

### 5.5 Migration

None. `position` column already exists.

---

## 6. Frontend Design

### 6.1 `CampaignFlow.tsx`

- Add reorder affordances. Two acceptable approaches (pick one):
  - **A. Drag-and-drop** (preferred, Discord-like feel): each step card is draggable;
    dropping reorders the local `steps` state and calls the API on drop.
  - **B. Up/down buttons** (simpler, matches existing `MoveBranch` precedent in
    `AddCondition`): a small ▲/▼ on each card header that swaps position with the
    neighbor and calls the API.
- After a successful reorder, update local state and invalidate the sequence query
  (`queryClient.invalidateQueries`) so realtime/other tabs reflect it.
- Preserve existing: add (+), delete, edit content, add branches, edit waits.

### 6.2 Sequence template builder (`web/src/components/app/flow/`)

- Mirror the reorder affordance in the template builder's step list if it exposes step
  ordering. Reuse the same API.

### 6.3 API client

Add `reorderSteps(campaignId, stepIds)` to the API client used by the builder
(`web/src/lib/api/...` — locate the existing `updateStep`/`createStep` client).

---

## 7. Tests (TDD — write before implementation)

### 7.1 Backend (Go)

- `internal/app/sequence/reorder_test.go` (or extend `handler_test.go`):
  - `TestReorderSteps_ReordersPosition`: create campaign with 3 steps, reorder, assert
    positions are the new order, gap-free, contiguous from 1.
  - `TestReorderSteps_RejectsUnknownStep`: 404 for a step ID not in the campaign.
  - `TestReorderSteps_RejectsDuplicate`: 400 for duplicate IDs.
  - `TestReorderSteps_RejectsMissing`: 400 when the requested list omits a real step.
  - `TestReorderSteps_OrgScoped`: a step from another org is rejected (404).
  - `TestReorderSteps_Audits`: an audit event is logged.
  - `TestReorderSteps_EntryStepFollowsPosition`: after reordering, the entry step is the
    new lowest-position step (verify via `GetSequences` / a helper that reads the entry).
- `internal/repository/pg_sequence_reorder_test.go` (if repo-layer tests exist):
  - transactional integrity — a failure mid-reorder rolls back.
- Routing-preservation test: reorder a branching sequence and assert the scheduler's
  routing output is unchanged (branch-first, else-next, linear-position+1). This pins the
  "reordering doesn't break branching" invariant.

### 7.2 Frontend (TypeScript)

- Locate existing flow/campaign tests (`web/src/components/app/campaigns/sequences/`,
  `web/src/components/app/flow/`). Add a test for the reorder handler:
  - `TestReorderStepsHandler` (or similar): simulate a drag/drop or up/down, assert the
    API is called with the new order and local state updates.
- Run `pnpm typecheck` and `pnpm lint` in `web/` after changes.

### 7.3 Verification gates (per AGENTS.md)

- Go: `make fmt` (`gofmt -l ./...` prints nothing), `make lint` (runs `check-migrations`).
- Frontend: `pnpm typecheck` + `pnpm lint` in `web/`.
- No new migration → `make check-migrations` stays green.

---

## 8. Docs

- `docs/content/docs/api/endpoints.mdx`: add `PATCH /campaigns/:id/steps/reorder` to the
  campaigns scope map with method + permission.
- `docs/content/docs/api/error-codes.mdx`: add `invalid_steps` (400) if we use that code
  (check existing codes; reuse `not_found`/`invalid_body` if a suitable one exists).
- `docs/content/docs/guides/` : add or update a page describing sequence step ordering if
  one exists; otherwise note in the campaign builder guide. Register in `guides/meta.json`
  if a new page.

---

## 9. Deployment / rollback compatibility

- Backward compatible: new endpoint is additive; existing endpoints unchanged.
- Old release keeps working (no reorder endpoint, no behavior change).
- New release accepts the new body shape; no schema/data change.
- No credential/codec/schema change.

---

## 10. Workspace data portability

- No new table → no `orgtransfer/spec.go` change needed.
- `position` already exists and travels with existing step data.

---

## 11. Open Questions

- **O1 — Entry step:** Should reordering be allowed to change the entry step (lowest
  position)? The user said "rearrange email sequence order," which implies yes. If the
  product wants the entry step locked, the endpoint rejects any order whose first element
  differs from the current entry. **Recommendation: allow it.**
- **O2 — Reorder granularity:** Full-list reorder (send all step IDs in new order) vs.
  pairwise swap (move step N to position M). **Recommendation: full-list** — atomic,
  matches drag-and-drop, simpler validation.
- **O3 — Frontend approach:** Drag-and-drop vs. up/down buttons. **Recommendation: start
  with up/down (lower risk, matches `MoveBranch` precedent), add drag if time permits.**
- **O4 — Error code:** Reuse existing `not_found`/`invalid_body` or add a dedicated
  `invalid_steps`? **Recommendation: add `invalid_steps` for clarity** (documented).

---

## 12. Acceptance Criteria

- A user can reorder steps in a campaign via the API and the UI.
- After reorder, the entry step, canvas order, and routing reflect the new order.
- Reordering a branching sequence does not change its routing behavior.
- Positions are always gap-free, contiguous, duplicate-free.
- All existing tests pass; new tests added; lint/typecheck clean.
- Docs updated.

---

## 13. Implementation Order (TDD)

1. Write backend tests (7.1) — expect them to fail.
2. Implement `ReorderSteps` service + repo + route + handler.
3. Make backend tests pass; run `make fmt` + `make lint`.
4. Write frontend tests (7.2).
5. Implement `CampaignFlow.tsx` + template builder reorder + API client.
6. Make frontend tests pass; `pnpm typecheck` + `pnpm lint`.
7. Update docs (8).
8. Commit (`feat: ...`), open PR to `development`.
