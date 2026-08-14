# Commercial Action Cockpit

Warmbly turns extra-cli Decision-Unit + Reachability into human commercial
work. It does not discover people, invent identity, or auto-send.

```
extra-cli  WHO + WHY NOW + DECISION UNIT + REACHABILITY
    ↓
Warmbly    what is the next commercial action?
    ↓
human executes CALL / ROUTED_CALL / EMAIL / ROLE_EMAIL / WHATSAPP /
            PROFESSIONAL_SOCIAL / CONTACT_FORM / OTHER
    ↓
OUTCOME + structured feedback upstream
```

Actionability and email sendability are independent. Missing a `VALIDATED`
email is not "no commercial work".

## Email guards (unchanged)

`VALIDATED` + messageability `READY` → `NEEDS_REVIEW` → human approval →
dispatch. Generic, role, inferred, and blocked recipients never promote to
validated email. Kill switch, dispatch pause, rate limits, stop-on-reply,
suppression, and `ResolveRecipient` stay fail-closed.

## Reachability mapping (`confenge.reachability.v1`)

Warmbly accepts optional additive fields on the current `confenge.outreach.v1`
feed. Unknown fields are ignored. An omitted `reachability_class` is left
empty (current contact-tier contract). An unknown non-empty class maps to
`UNMAPPED` (no auto-send).

| Upstream token | Canonical class | Action | Lane |
| --- | --- | --- | --- |
| `R1`, `R1_DIRECT` | `R1_DIRECT` | `DIRECT_EMAIL` | `EMAIL_NEEDS_REVIEW` only if VALIDATED+READY |
| `R2`, `R2_HIGH_CONFIDENCE_DIRECT`, `INFERRED_DIRECT` | `R2_HIGH_CONFIDENCE_DIRECT` | `INFERRED_EMAIL_REVIEW` | `HUMAN_REVIEW_EMAIL` (never VALIDATED, never dispatch) |
| `R3`, `R3_ROUTED_TO_NAMED_PERSON`, `ROUTES_TO_NAMED_PERSON` | `R3_ROUTED_TO_NAMED_PERSON` | `ROUTED_CALL` | `ROUTED_CALL_QUEUE` |
| `R4`, `R4_ROLE_ROUTE`, `ROLE_MAILBOX` | `R4_ROLE_ROUTE` | `ROLE_EMAIL` | `ROLE_EMAIL_QUEUE` |
| `R5`, `R5_CORPORATE_ONLY` | `R5_CORPORATE_ONLY` | `GENERIC_EMAIL` / `CONTACT_FORM` | `LOW_CONFIDENCE_MANUAL` |
| `R0`, `NO_ACTIONABLE_ROUTE` | `R0_NO_ACTIONABLE_ROUTE` | none | none |
| `BLOCKED`, `DNC` | `BLOCKED` | blocked | `BLOCKED` |

Published extra-cli ActionMode tokens map the same way. `MANUAL_ROUTED_CALL`
is `R3` and is executable without a VALIDATED email. `DIRECT_EMAIL_VALIDATED`
still requires VALIDATED + messageability READY + human approval before
`EmailSendable`. `NAMED_HUMAN_MANUAL_CHANNEL` is a first-class manual lane.
`ROLE_MAILBOX` / `GENERIC` stay exception or low-confidence manual. Unknown
tokens fail closed to `UNMAPPED`.

A named person plus a company phone without `BELONGS_TO_NAMED_PERSON` is
`ROUTED_CALL`, never a direct phone.

`confenge import` also accepts the extra-cli operator pack (`cards.json`) and
`confenge.decision_unit_account.v1` accounts. Re-import is idempotent. Warmbly
keeps extra-cli account id, person id, evidence ids, why-now, route class,
confidence, recommended action, and service. It does not invent a person,
role, email, or phone.

After import the CLI prints an operator summary: actionable accounts, route
distribution, manual-call count, email-safe count, unresolved blockers, and
the next human actions.

## Plug contract for extra-cli (additive)

Keep shipping `confenge.outreach.v1`. Optionally add, per lead or contact:

```json
{
  "decision_unit_candidates": [],
  "reachability_routes": [],
  "recommended_target": {},
  "recommended_route": {},
  "recommended_action": "ROUTED_CALL",
  "contacts": [{
    "reachability_class": "R3_ROUTED_TO_NAMED_PERSON",
    "route_type": "phone",
    "route_relation": "ROUTES_TO_NAMED_PERSON",
    "channel_value": "+554132220000",
    "channel_display": "telefone oficial da empresa",
    "inferred_email": false
  }]
}
```

Do not invent a person when only a role is known. Warmbly will target the
function, not a fabricated name.

## Outcomes back to extra-cli

`confenge.outcome.v1` is extended additively. New top-level fields (optional):

- `action_id`, `action_type`, `reachability_class`, `outcome_code`
- `target_reached`, `conversation_started`, `interest_state`
- `person_relevance_feedback`, `route_validity`
- `referral` `{name, role, followup_action_id}`
- `new_person`, `new_role`, `new_route`, `preferred_channel`

WON is never inferred from `INTERESTED` or `MEETING_SCHEDULED`.

## Lanes

`EMAIL_NEEDS_REVIEW`, `HUMAN_REVIEW_EMAIL`, `CALL_QUEUE`, `ROUTED_CALL_QUEUE`,
`WHATSAPP_QUEUE`, `PROFESSIONAL_SOCIAL_QUEUE`, `ROLE_EMAIL_QUEUE`,
`CONTACT_FORM_QUEUE`, `LOW_CONFIDENCE_MANUAL`, `NEEDS_ENRICHMENT`, `BLOCKED`,
`DONE`.

## What this version does not do

No dialer, WhatsApp bot, LinkedIn bot, form submitter, CRM, calendar, or
forecasting. No person discovery inside Warmbly.
