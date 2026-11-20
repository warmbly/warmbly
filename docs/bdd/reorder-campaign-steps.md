# BDD: Reorder Campaign Sequence Steps

Behavior-Driven Development scenarios for the reorder feature.
Written in Given/When/Then (Gherkin-flavored) so behavior is specified before code.
Feature: **Rearranging email sequence order within a campaign.**

---

## Feature: Reorder steps in a campaign sequence

As a **campaign manager**,
I want to **rearrange the order of my campaign's email steps**,
So that **I can control the sequence in which contacts receive messages.**

---

### Scenario 1: Reorder a linear sequence changes the order contacts receive emails

**Given** a campaign with three steps in order:
- Step A "Greeting" (position 1)
- Step B "Follow-up" (position 2)
- Step C "Check-in" (position 3)

**When** I reorder the steps to B, C, A (via the builder UI or the reorder API)

**Then** the steps are now in order:
- Step B "Follow-up" (position 1)
- Step C "Check-in" (position 2)
- Step A "Greeting" (position 3)

**And** a new contact entering the campaign receives Step B first, then C, then A
(the catch-all chain now follows the new order).

---

### Scenario 2: Reordering preserves conditional branches

**Given** a campaign where Step A has a conditional branch:
- If the recipient opens A → go to Step B (conditional branch)
- Otherwise → go to Step C (catch-all / "go there next")

**When** I reorder the steps so that C comes before A

**Then** the conditional branch on A still routes an opener to B

**And** the catch-all on A still routes a non-opener to C

**And** the steps are now ordered C, A, B (positions updated)

**And** no branch is lost, duplicated, or pointed at a non-existent step

---

### Scenario 3: The entry step follows the new order

**Given** a campaign where Step A is the entry step (lowest position)

**When** I reorder so that Step C is first

**Then** a new contact enters at Step C

**And** the canvas lists Step C first

---

### Scenario 4: Reorder via the API with a full ordered list

**Given** a campaign with steps A, B, C

**When** I send `PATCH /campaigns/:id/steps/reorder` with body
`{ "stepIds": ["C", "A", "B"] }`

**Then** the response is `200 OK`

**And** the steps are reordered to C, A, B

**And** positions are gap-free and contiguous (1, 2, 3) with no duplicates

---

### Scenario 5: Reorder rejects an unknown step

**Given** a campaign with steps A, B, C

**When** I send `PATCH /campaigns/:id/steps/reorder` with body
`{ "stepIds": ["C", "A", "B", "Z"] }` where Z is not a step in the campaign

**Then** the response is `404 Not Found`

**And** no steps are reordered

---

### Scenario 6: Reorder rejects duplicate or missing step IDs

**Given** a campaign with steps A, B, C

**When** I send a reorder with `{ "stepIds": ["A", "A", "B"] }` (duplicate, missing C)

**Then** the response is `400 Bad Request`

**And** no steps are reordered

---

### Scenario 7: Reorder is scoped to the organization

**Given** a contact's authenticated session for Organization 1

**When** I attempt to reorder steps belonging to Organization 2's campaign

**Then** the response is `404 Not Found`

**And** Organization 1's campaigns are unaffected

---

### Scenario 8: Reorder is audited

**Given** a campaign manager reorders steps in a campaign

**When** the reorder succeeds

**Then** an audit event is logged:
- action: update
- entity type: `campaign_step` (or `campaign_sequence`)
- entity id: the campaign id
- metadata: the new step order

---

### Scenario 9: Reorder via the builder UI

**Given** a campaign manager viewing a campaign's sequence builder

**When** I drag Step B above Step A (or click the "move up" control on Step B)

**Then** the steps visually reorder in the builder

**And** the reorder is persisted (the API call succeeds)

**And** other collaborators see the new order (via realtime invalidation)

---

### Scenario 10: Reordering a branching sequence does not change its routing

**Given** a campaign with conditional branches and a catch-all chain

**When** I reorder the steps

**Then** every conditional branch still routes to its original target

**And** the catch-all chain now follows the new position order

**And** no contact is stranded (every step is reachable from the entry)

---

### Scenario 11: The entry step cannot be removed from the flow

**Given** a campaign with steps A, B, C

**When** I reorder so that any single step is first

**Then** the entry step is always the first step in the order

**And** every step remains reachable from the entry

---

## Acceptance (Definition of Done)

- Scenarios 1–11 all pass.
- Positions are always gap-free, contiguous, duplicate-free.
- Reordering a branching sequence preserves all conditional branches.
- The reorder is org-scoped and audited.
- Existing campaign routing behavior is unchanged for branching sequences.
- Lint, typecheck, and all existing tests pass.
