// The automatic label set with the colours the backend files them under
// (internal/repository/pg_inbox_tag_categories.go) and the hover meaning the
// dashboard shows (web/src/lib/unibox/tagMeanings.ts).

export type Label = { t: string; c: string; m?: string };

export const L = {
  interested: { t: 'Interested', c: '#15803d', m: 'They said yes to the call, the partnership, or the next step.' },
  meeting: { t: 'Meeting', c: '#15803d', m: 'They asked for a call or meeting, or proposed or confirmed a time.' },
  pricing: { t: 'Pricing', c: '#0d9488', m: 'They asked about price, terms, or commercials.' },
  question: { t: 'Question', c: '#0284c7', m: 'Open to it, but asking questions before deciding.' },
  update: { t: 'Update', c: '#0284c7', m: 'News on something already agreed, or the answer to something you asked.' },
  notNow: { t: 'Not now', c: '#a16207', m: 'Interested in principle, but says the timing is wrong.' },
  notInterested: { t: 'Not interested', c: '#9f1239', m: 'They declined. Never chased again.' },
  wrongPerson: { t: 'Wrong person', c: '#7c3aed', m: 'Not the right contact, or they named someone else.' },
  unsubscribe: { t: 'Unsubscribe', c: '#b91c1c', m: 'They asked to stop receiving email, or complained. Never chased again.' },
  bounced: { t: 'Bounced', c: '#b91c1c' },
  ooo: { t: 'Out of office', c: '#a16207' },
  autoReply: { t: 'Auto-reply', c: '#a16207' },
  notification: { t: 'Notification', c: '#64748b' },
  actionRequired: { t: 'Action required', c: '#dc2626' },
  needsReply: { t: 'Needs reply', c: '#be123c' },
  followUp: { t: 'Follow up', c: '#c2410c' },
} satisfies Record<string, Label>;
