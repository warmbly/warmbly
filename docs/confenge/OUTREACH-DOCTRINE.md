# CONFENGE outreach doctrine (confenge-outreach-v2)

Machine-readable twin: `internal/app/confenge/outreach_playbook/` (mirrored under `data/confenge/outreach_playbook/`).

Constant: `OutreachDoctrineVersion` in `internal/app/confenge/playbook.go`.

## Purpose

Maximize:

```
meaningful positive conversations ÷ emails effectively delivered
```

Not volume, not prettier templates, not fake personalization.

## Architectural boundary

| Plane | Owns |
| --- | --- |
| extra-cli | WHO / WHY NOW (activation, ranking) |
| Warmbly strategy | WHAT TO SAY / WHAT TO OFFER / WHAT NEXT |

Warmbly does **not** create `lead_score`, `commercial_score`, or re-rank activation.

## Pipeline

```
intelligence (dossier)
  → commercial strategy (OutreachStrategy, internal)
  → messageability gate (READY | NEEDS_ENRICHMENT | BLOCKED)
  → outbound-safe plan (hook, relevance, value_unit, cta)
  → constrained generation (plan only)
  → deterministic hard QA
  → human approval (authorization, not rewrite)
  → send (existing gates)
  → outcomes / experiments
```

Never: lead data → freestyle LLM → email.
Never: interpolate ProblemHypothesis / metadata dumps into copy.
`NEEDS_REVIEW` means the message is already sendable and only human authorization remains.

## First email

Sells only: attention + relevance + rational curiosity + low-friction next step.

Does **not** sell: consulting package, retainer, paid diagnostic, or meeting by default.

Structure:

1. Relevant public observation (minimal fact)
2. Honest implication / hypothesis (not accusation)
3. Useful perspective (Challenger insight, not fearmongering)
4. Micro-offer CTA (permission / interest)

Defaults (experimental priors): ~40–100 words, 3–4 sentences, one CTA, PT-BR, no attachment.

## Fact vs hypothesis

- Facts: evidence-backed, public, traceable
- Hypothesis: linguistically scoped (`parece`, `talvez valha conferir`, `não dá para concluir só com a publicação`)
- Annualidade alone = verify-now signal, **never** “reajuste a receber”

## Anti-creepy

`KNOW A LOT / SAY LITTLE`. Prefer “Vi no PNCP…” / “Pelo contrato publicado…”. Never “estamos monitorando sua empresa”.

## Micro-offers

Only LOW fulfillment budget on cold path. Fulfill promised value before asking for a meeting.

## Sequence (5 touches)

1. SIGNAL — fact + hypothesis + micro-offer  
2. IMPLICATION — new angle  
3. VALUE — checklist / insight  
4. ROUTING — internal referral when appropriate  
5. GRACEFUL CLOSE — no guilt trip  

No empty “just following up”.

## Experiments

Primary metric: positive conversation rate (not opens).  
Account-level assignment; single dimension when possible; small-n → INCONCLUSIVE.

## Safety non-goals of this layer

Does not change: governor, green_autorun, policy_auth, transport, activation scoring, DNC transport gate, WhatsApp runtime (stays OFF).
