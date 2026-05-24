// Built-in starter templates. Picked from the editor when the user does
// not want to start blank. The copy is intentionally conversational so
// the saved template feels like something a person wrote, not a tool.
//
// Editing notes:
//  - keep variables in the {{.FirstName}} form so they line up with the
//    backend renderer in internal/app/template/service.go
//  - sign-offs use [your name] as a placeholder the user replaces once

export interface TemplatePreset {
    id: string;
    label: string;
    tag: string;
    description: string;
    name: string;
    subject: string;
    body_plain: string;
}

export const TEMPLATE_PRESETS: TemplatePreset[] = [
    {
        id: "cold-intro",
        label: "Cold intro",
        tag: "Sales",
        description: "First touch, no prior context",
        name: "Cold intro · Product",
        subject: "Quick question, {{.FirstName}}",
        body_plain:
`Hi {{.FirstName}},

I was reading up on what {{.Company}} is doing and figured this was worth a quick note. We help teams in your shape sort out [the problem you solve], usually without adding more tools to the stack.

Worth a 15 minute call next week to see if it lines up? Happy to send a few times that work on my side.

Thanks,
[your name]`,
    },
    {
        id: "follow-up",
        label: "Follow up",
        tag: "Sales",
        description: "Bump after 3 days of silence",
        name: "Follow-up · 3 days",
        subject: "Re: Quick question",
        body_plain:
`Hey {{.FirstName}},

Just bumping this up in case it got buried earlier in the week. No worries if the timing is off, totally get it.

Still happy to walk you through how we'd think about it if you can spare 15 minutes.

Thanks,
[your name]`,
    },
    {
        id: "soft-close",
        label: "Soft close",
        tag: "Sales",
        description: "Last message in a sequence, no pressure",
        name: "Final follow-up · soft close",
        subject: "Should I close this out?",
        body_plain:
`Hi {{.FirstName}},

Haven't heard back so I'll go ahead and stop poking. Not a problem, timing matters more than the pitch.

If something changes down the road, you know where to find me.

All the best,
[your name]`,
    },
    {
        id: "re-engage",
        label: "Re-engagement",
        tag: "Nurture",
        description: "Reach out after a long quiet stretch",
        name: "Re-engagement · 30 days",
        subject: "Still on your radar?",
        body_plain:
`Hi {{.FirstName}},

It's been a while since we last spoke. A few things have shifted on our side that might change the picture for {{.Company}}.

Want me to send a short note on what's new, or skip it for now?

Thanks,
[your name]`,
    },
    {
        id: "meeting-confirm",
        label: "Meeting confirm",
        tag: "Ops",
        description: "Confirm a call that is already booked",
        name: "Meeting confirm",
        subject: "Looking forward to our chat",
        body_plain:
`Hi {{.FirstName}},

Just confirming our call. The calendar invite is already in your inbox.

If anything comes up and you need to move it, just send a note and we'll find another slot.

Talk soon,
[your name]`,
    },
    {
        id: "thanks-reply",
        label: "Thanks reply",
        tag: "Reply",
        description: "Acknowledge a reply and ask one follow-up",
        name: "Thanks for the reply",
        subject: "Re: thanks",
        body_plain:
`Hi {{.FirstName}},

Thanks for getting back to me, appreciate it.

Quick follow-up: [one specific question that unlocks the next step]. Once I have that I can put together something useful instead of generic.

Thanks,
[your name]`,
    },
    {
        id: "polite-no",
        label: "Polite no thanks",
        tag: "Reply",
        description: "Close out a decline graciously",
        name: "Polite reply · no thanks",
        subject: "Re: thanks for letting me know",
        body_plain:
`Hi {{.FirstName}},

No problem at all, thanks for being upfront. If things change down the road, my inbox is open.

All the best,
[your name]`,
    },
    {
        id: "intro-ask",
        label: "Intro ask",
        tag: "Nurture",
        description: "Ask for a referral inside their company",
        name: "Intro ask",
        subject: "Quick favor",
        body_plain:
`Hi {{.FirstName}},

Long shot, but is there someone on your team who handles [topic]? I can keep it short and easy for them, just need a name and I'll take it from there.

Thanks either way,
[your name]`,
    },
];
