# Per-touchpoint human approval

Every CONFENGE outbound (email or WhatsApp) requires explicit human approval of
that exact message before queue/send. AI never writes `approved_by`.

States: PLANNED → DUE → DRAFTED/NEEDS_REVIEW → APPROVED → QUEUED → SENT.
Terminals: SKIPPED, REJECTED, REPLIED, DNC, BOUNCED, CANCELLED, FAILED.

Transport requires `approved_content_hash == content_hash` and human `approved_by`.
Edit/regenerate clears approval. CAS on APPROVED→QUEUED. Migration `000085`.
Cadence policy `confenge.cadence.v1` (no fixed multi-step campaign sequences).

## Fail-closed transport

`EnrollDraft` (email) and `SendApprovedWhatsApp` both call `requireTouchTransport`.
A draft with status `APPROVED` but **no** linked touchpoint is refused. Linked
touchpoints must have matching content hashes and human `approved_by`.

PLANNED touches with `due_at <= now` are promoted to `DUE` when listing the
review queue (`PromoteDuePlannedTouchpoints`), so spaced follow-ups enter the
human queue after the prior touch is SENT/SKIPPED and the delay elapses.

## Prior-touch release

The next ordinal is never promoted to `DUE` (and cannot be approved/queued)
while any lower ordinal is still open. Only `SENT`, `SKIPPED`, or `REJECTED`
on every prior step releases the next. `PromoteDuePlannedTouchpoints` respects
this filter.

## Content bind

Transport re-hashes the live draft subject/body/recipient against the touchpoint
`approved_content_hash`. Editing the draft after approval clears touchpoint
approval and blocks send until re-approve.
