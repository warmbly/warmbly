// What each automatic label means, in one sentence a person can read on hover.
//
// The wording mirrors the criteria the classifier is actually given
// (internal/app/inboxtag/policy.go), so the explanation and the rule cannot
// drift into saying different things. Keyed on the lowercased label title,
// which is what the workspace files under.
// ponytail: a hand-kept map; move it to a `categories.description` column if
// labels a user created ever need their own explanations too.

const TAG_MEANINGS: Record<string, string> = {
    // What the message is. Automated mail leaves the inbox for the Automated view.
    bounced: "The email was not delivered: the address does not exist, or the server refused it for now.",
    "out of office": "An automatic out-of-office or vacation reply, not a person.",
    "auto-reply": "An automatic \"we got your message\" or ticket receipt, not a person.",
    notification: "An automatic message from a service: security alerts, sign-in codes, receipts, newsletters.",
    "sales pitch": "Someone trying to sell to you. Not a reply to your outreach.",

    // What a reply wants. Only ever on a person's reply.
    interested: "They said yes to the call, the partnership, or the next step.",
    meeting: "They asked for a call or meeting, or proposed or confirmed a time.",
    pricing: "They asked about price, terms, or commercials.",
    question: "Open to it, but asking questions before deciding.",
    update: "News on something already agreed, or the answer to something you asked.",
    "not now": "Interested in principle, but says the timing is wrong.",
    "not interested": "They declined. Never chased again.",
    "wrong person": "Not the right contact, or they named someone else.",
    unsubscribe: "They asked to stop receiving email, or complained. Never chased again.",
    "legal threat": "They threatened legal action or named a regulator.",

    // Follow-up state. Computed from your mailbox, not from a model, and kept
    // in sync as time passes.
    "needs reply": "They replied and nobody has answered for two days or more.",
    "follow up": "You wrote last, five days ago, and heard nothing back.",
    "gone quiet": "They were interested, then went quiet for ten days. Worth a nudge.",

    // The one that means the system declined to decide.
    "needs review": "The classifier was not sure about this one. Read it yourself.",
};

/**
 * tagMeaning returns the hover explanation for a label, or "" for a label this
 * workspace created itself, which needs no explaining to the person who made it.
 */
export function tagMeaning(title: string): string {
    return TAG_MEANINGS[title.trim().toLowerCase()] ?? "";
}

/** isAutomaticTag reports whether a label is one this system applies. */
export function isAutomaticTag(title: string): boolean {
    return tagMeaning(title) !== "";
}
