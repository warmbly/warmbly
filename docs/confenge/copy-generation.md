# CONFENGE copy generation (evidence-grounded)

Warmbly redacts and validates commercial copy from the **imported extra-cli dossier** only.
There is **no research** in this path: the model receives structured JSON and returns structured JSON.

## Prompt version

| Version | Notes |
| --- | --- |
| `confenge.draft.v1` | Initial evidence-bound email generator + template fallback |
| `confenge.draft.v2` | Channel-aware modes, `claims[]`, internal `rationale`, anti-template linter, near-dup single regen |
| `confenge.draft.v3` | Strategy-first composition (`OutreachStrategy`), doctrine `confenge-outreach-v1`, micro-offers, doctrine QA |
| `confenge.draft.v4` | Messageability gate + outbound-safe plan (`confenge.composer.v2`, doctrine `confenge-outreach-v2`). Internal strategy fields are never interpolated. Unsent prior-version drafts must be regenerated. |

Constant: `internal/app/confenge/validators.go` → `PromptVersion`.
Doctrine: `OutreachDoctrineVersion` + `internal/app/confenge/outreach_playbook/`.

Bump the constant when the system prompt schema or hard safety rules change. Store the version on each `OutreachDraft.PromptVersion` for audit.

## Generation channels

| Kind | Persist channel | When |
| --- | --- | --- |
| `EMAIL_INITIAL` | EMAIL | First outbound email |
| `EMAIL_FOLLOWUP` | EMAIL | Same thread after ignored mail |
| `WHATSAPP_INITIAL` / `WHATSAPP_CONTINUATION` | WHATSAPP | Only if policy/consent allows generation |
| `REPLY_DRAFT` | EMAIL | Lead already replied; reuses confenge generate path (not unibox auto-send) |

WhatsApp generation **short-circuits** when consent/policy blocks the channel (`CONFENGE_WHATSAPP_ENABLED`, opt-in provenance, DNC). Free-text still never auto-sends without human approval.

## Inputs (dossier only)

- company (razao/nome/UF)
- contact + role + verification
- service (`service_code` / name / entry offer)
- `why_now` / moment summary
- confirmed + inferred evidence rows (`epistemic_class`)
- internal-structure hypothesis (hypothesis evidence → question only)
- `claims_to_avoid`
- optional touch / reply history
- recent draft bodies (near-dup fingerprints only)

## Output schema

```json
{
  "channel": "EMAIL_INITIAL",
  "subject": "...",
  "body_text": "...",
  "body_html": "",
  "followups": [],
  "fact_used": "...",
  "evidence_ids": ["ev-1"],
  "claims": [{"phrase": "...", "fact": "...", "evidence_ids": ["ev-1"]}],
  "service_code": "ADDITIVE_REVIEW",
  "question": "...",
  "cta": "...",
  "risk_flags": [],
  "rationale": "operator-only; never sent to the lead"
}
```

`body_html` is optional and must be safe (no scripts). The pipeline currently clears model HTML on save unless a future sanitizer is added.

Claims + rationale are packed into `ValidationJSON` (no new migration).

## Deterministic validators

Hard fails include:

- unknown `evidence_id`
- `service_code` mismatch without audited human override
- hypothesis stated as hard fact ("sei que vocês não têm equipe")
- em/en dashes, banned phrases, financial promises, invented urgency
- anti-template / AI clichés / generic subjects / artificial company-name spam
- excessive paragraphs or bullets
- WhatsApp too long or looking like a pasted email

Near-duplicate (character n-gram Jaccard, threshold `0.72`):

- marks risk / warning
- allows **one** structure/hook-oriented regeneration
- never loops

## Provider abstraction

`AIDraftGenerator` uses only `generation.Provider.Complete` (no tools, no web search).
`TemplateGenerator` is the offline fallback and still passes the same linters.

## Feature flags

| Variable | Default | Meaning |
| --- | --- | --- |
| `CONFENGE_OUTREACH_ENABLED` | false | Master switch |
| `CONFENGE_REQUIRE_HUMAN_APPROVAL` | true | AI never approves/sends |
| `CONFENGE_AUTO_SEND_ENABLED` | false | Fail-closed |
| `CONFENGE_MAX_INITIAL_EMAIL_WORDS` | 120 | Hard word cap |
| `CONFENGE_WHATSAPP_ENABLED` | false | WA generate/send |
| `CONFENGE_MAX_WHATSAPP_WORDS` | 70 | WA word cap |

## Non-claims

- AI may draft; **humans** approve and send.
- DNC, opt-out, bounce, and reply remain dominant cadence stops.
- Multi-tenant isolation is enforced by org-scoped repositories.
- Every sent message must remain traceable to lead, contact, channel, text version, approval, and evidence ids.
