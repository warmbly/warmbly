// What the product tour says: chapters of slides, in order. A slide's anchor
// is the data-tour attribute of what it spotlights (a sidebar row's url, or
// "remie" for the assistant button); a slide without one, or whose element is
// not on screen, lights nothing. Remie narrates every slide, and `ask` is the
// same job handed to Remie instead of clicked through.

export type SceneId =
    | "welcome"
    | "addAccount"
    | "accounts"
    | "warmup"
    | "domains"
    | "campaignList"
    | "campaignSteps"
    | "campaignLive"
    | "contacts"
    | "segments"
    | "inbox"
    | "inboxDrafts"
    | "analytics"
    | "deliverability"
    | "deals"
    | "automationList"
    | "automationBuilder"
    | "remieAsk"
    | "remieAct"
    | "slackAsk"
    | "slackInbox"
    | "planIncluded"
    | "planCompare"
    | "plans";

export interface TourSlide {
    id: string;
    scene: SceneId;
    anchor?: string;
    /** Names the spotlit element ("In the sidebar: Accounts"). */
    label?: string;
    /** Grey lead-in before the title, as on the marketing site. */
    lead: string;
    title: string;
    body: string;
    points: string[];
    /** Remie's own line on this slide. */
    remie?: string;
    ask?: string;
    /** An example message for @Warmbly, on the Slack slides. */
    slack?: string;
}

export interface TourChapter {
    id: string;
    name: string;
    slides: TourSlide[];
}

export const TOUR_CHAPTERS: TourChapter[] = [
    {
        id: "welcome",
        name: "Welcome",
        slides: [
            {
                id: "welcome",
                scene: "welcome",
                lead: "Welcome to Warmbly.",
                title: "Here is how it all fits together.",
                body: "Warmbly warms up the mailboxes you send from, runs your campaigns across them and brings every reply into one place. This tour walks through each part of the dashboard on a sample workspace, so you know what everything does before you set up your own.",
                points: [
                    "Click through the dashboard yourself",
                    "Ask Remie, the AI assistant, to do it for you",
                    "Or message @Warmbly in Slack",
                ],
                remie: "Hi, I'm Remie. I'll show you around. Anything you see on this tour, you can also just ask me to do.",
            },
        ],
    },
    {
        id: "mailboxes",
        name: "Mailboxes",
        slides: [
            {
                id: "connect",
                scene: "addAccount",
                anchor: "/app/emails",
                label: "Accounts",
                lead: "Mailboxes.",
                title: "Connect every inbox you send from.",
                body: "Sign in with Google or Microsoft in one click, connect any other host over SMTP and IMAP, import a list of mailboxes from a spreadsheet, or pull them straight from an inbox vendor like InboxKit or Zapmail.",
                points: [
                    "Admins can connect a whole Google or Microsoft domain at once",
                    "Server settings are detected for you on import",
                    "Your mail still goes out through your own provider",
                ],
                remie: "Start here. Once a mailbox is connected, I can warm it up, check its domain and put it to work.",
            },
            {
                id: "overview",
                scene: "accounts",
                anchor: "/app/emails",
                label: "Accounts",
                lead: "Accounts.",
                title: "Every mailbox, at a glance.",
                body: "Each mailbox shows what it sent today against its own limit, its warmup progress, where its mail lands and its health. Sending is spread across all of them, so no single inbox sends too much.",
                points: [
                    "50 cold emails a day per mailbox by default, at least 10 minutes apart",
                    "A struggling mailbox is slowed down on its own, before it does harm",
                    "Tags like VIP or Agency choose which mailboxes a campaign sends from",
                ],
                ask: "Which of my mailboxes need attention, and why?",
            },
            {
                id: "warmup",
                scene: "warmup",
                anchor: "/app/emails",
                label: "Accounts",
                lead: "Warmup.",
                title: "A slow, steady ramp, and a read on placement.",
                body: "Warmup sends short, disclosed test messages between participating mailboxes and records where they land at Google, Microsoft and Yahoo. It starts at 10 a day and adds one a day, up to 40, and shows a configuration or delivery problem before a campaign finds it.",
                points: [
                    "Turn it on per mailbox from its Warmup tab",
                    "Up to five a day keep running alongside a live campaign",
                    "A mailbox landing in spam is slowed down, not cut off",
                ],
                ask: "Turn on warmup for every mailbox that isn't warming yet",
            },
            {
                id: "domains",
                scene: "domains",
                anchor: "/app/emails",
                label: "Accounts",
                lead: "Sending domains.",
                title: "Domains that are set up right.",
                body: "Sending domains lists every domain you send from and whether SPF, DKIM and DMARC are published, plus the tracking domain that keeps your open and click links on your own brand.",
                points: [
                    "A domain failing authentication is flagged before it costs you",
                    "Your own tracking domain instead of a shared one",
                    "Redirects for domains you only use to send",
                ],
                ask: "Check my sending domains and tell me what is missing",
            },
        ],
    },
    {
        id: "campaigns",
        name: "Campaigns",
        slides: [
            {
                id: "list",
                scene: "campaignList",
                anchor: "/app/campaigns",
                label: "Campaigns",
                lead: "Campaigns.",
                title: "All of your outreach, in one list.",
                body: "A campaign is a list of contacts, a sequence of emails and a schedule. See what is sending, paused, drafted or finished, and file campaigns into folders as they pile up.",
                points: [
                    "Pause and resume any time without losing anyone's place",
                    "Folders, search and filters once there are many",
                    "Every number updates live as mail goes out",
                ],
                ask: "Which of my campaigns gets the most replies?",
            },
            {
                id: "steps",
                scene: "campaignSteps",
                anchor: "/app/campaigns",
                label: "Campaigns",
                lead: "Steps.",
                title: "Sequences that know when to stop.",
                body: "Write the first email and the follow-ups. Each follow-up waits the days you choose and replies in the same thread, so it reads like a real conversation. With Stop on reply on, anyone who answers leaves the sequence.",
                points: [
                    "Variables and AI-written lines personalise every email",
                    "Branch the flow on what each contact did",
                    "Start from a saved template, or have AI write the first draft",
                ],
                ask: "Write a three-step campaign for agency founders",
            },
            {
                id: "live",
                scene: "campaignLive",
                anchor: "/app/campaigns",
                label: "Campaigns",
                lead: "Overview.",
                title: "Watch it send, live.",
                body: "A running campaign shows how much went out today and why, then opens, clicks, replies and bounces as they happen. A mailbox that is still warming up is eased in, never pushed past what it can take.",
                points: [
                    "Sending stays inside your days and hours, in your timezone",
                    "Leads shows where every contact is in the sequence",
                    "Bounces and unsubscribes are suppressed automatically",
                ],
                ask: "How is Sunrise Q3 launch outreach doing?",
            },
        ],
    },
    {
        id: "contacts",
        name: "Contacts",
        slides: [
            {
                id: "list",
                scene: "contacts",
                anchor: "/app/contacts",
                label: "Contacts",
                lead: "Contacts.",
                title: "Everyone you reach, in one place.",
                body: "Import a CSV or spreadsheet, sync a Google Sheet, or let forms and integrations add people for you. Every contact carries a timeline of each email, open and reply.",
                points: [
                    "Custom fields for anything your emails mention",
                    "Filter by label, segment, status or campaign",
                    "Duplicates are merged, never imported twice",
                ],
                ask: "Tag everyone who opened twice but never replied as warm",
            },
            {
                id: "segments",
                scene: "segments",
                anchor: "/app/contacts",
                label: "Contacts",
                lead: "Segments.",
                title: "Lists that keep themselves current.",
                body: "Segments are saved audiences that update as contacts change, so a campaign always targets the right people. The suppression list holds everyone who unsubscribed or bounced, and nothing is ever sent to them.",
                points: [
                    "Every email carries a one-click unsubscribe",
                    "Labels show where each person stands",
                    "Suppressed contacts are skipped by every campaign",
                ],
                ask: "Make a segment of contacts at agencies with 50 to 500 people",
            },
        ],
    },
    {
        id: "inbox",
        name: "Inbox",
        slides: [
            {
                id: "unibox",
                scene: "inbox",
                anchor: "/app/unibox",
                label: "Inbox",
                lead: "Inbox.",
                title: "Every reply, from every mailbox.",
                body: "Replies from all of your mailboxes land in one inbox, threaded, with the mailbox they came to. Filter by mailbox, label or tag, snooze what can wait and archive what is done.",
                points: [
                    "Labels like Lead, Customer and Churn risk",
                    "Keyboard shortcuts for everything",
                    "Teammates see who is already replying",
                ],
                ask: "Summarise today's replies and tell me who to answer first",
            },
            {
                id: "drafts",
                scene: "inboxDrafts",
                anchor: "/app/unibox",
                label: "Inbox",
                lead: "Agent drafts.",
                title: "Answer faster, with a draft ready.",
                body: "When someone replies, the inbox agent can write a suggested answer in your voice and hold it for you. Approve it, edit it or throw it away. It never sends on its own.",
                points: [
                    "Agent drafts lists every conversation with a draft waiting",
                    "Replies go out from the mailbox that received the email",
                    "Undo send gives you a few seconds to change your mind",
                ],
                ask: "Draft replies to everyone who asked about pricing this week",
            },
        ],
    },
    {
        id: "analytics",
        name: "Analytics",
        slides: [
            {
                id: "analytics",
                scene: "analytics",
                anchor: "/app/analytics",
                label: "Analytics",
                lead: "Analytics.",
                title: "See what actually works.",
                body: "Sent, opens, clicks, replies and bounces over time, for the whole workspace, one campaign or a single mailbox. Top campaigns shows where your replies really come from.",
                points: [
                    "7, 30 and 90 day views",
                    "Share any chart as an image",
                    "Account health across every mailbox",
                ],
                ask: "Why did my reply rate drop this week?",
            },
            {
                id: "deliverability",
                scene: "deliverability",
                anchor: "/app/deliverability",
                label: "Deliverability",
                lead: "Deliverability.",
                title: "Know where your mail lands.",
                body: "Bounces, complaints and spam placement rolled into one score, and where warmup mail landed at Google and Microsoft. When something slips, the Advisor points at the fix on the page where it lives.",
                points: [
                    "Bounce and complaint rates against provider limits",
                    "Inbox placement from real warmup deliveries",
                    "Placement tests before a big send",
                ],
                ask: "Is anything hurting my deliverability right now?",
            },
        ],
    },
    {
        id: "crm",
        name: "CRM",
        slides: [
            {
                id: "deals",
                scene: "deals",
                anchor: "/app/crm/deals",
                label: "Deals",
                lead: "CRM.",
                title: "Turn replies into revenue.",
                body: "Interested replies become deals, each with a contact, a stage and a value. Work them as a table or a board, with tasks and meetings beside each one. Already on HubSpot, Pipedrive or Salesforce? Connect it and Warmbly works with your records there.",
                points: [
                    "Pipelines with your own stages",
                    "Tasks and meetings tied to the conversation",
                    "Open and won value at a glance",
                ],
                ask: "Create a deal for every interested reply this week",
            },
        ],
    },
    {
        id: "automations",
        name: "Automations",
        slides: [
            {
                id: "list",
                scene: "automationList",
                anchor: "/app/automations",
                label: "Automations",
                lead: "Automations.",
                title: "Let the busywork run itself.",
                body: "An automation starts on something that happens, like a reply, a booked meeting, a form or a bounce, and does the follow-through for you: open a deal, label a contact, push to your CRM, post to Slack.",
                points: [
                    "Ready-made templates for the common ones",
                    "Works across your CRM, Slack and other integrations",
                    "One failing step never blocks the rest",
                ],
                ask: "Build an automation that posts interested replies to Slack",
            },
            {
                id: "builder",
                scene: "automationBuilder",
                anchor: "/app/automations",
                label: "Automations",
                lead: "The builder.",
                title: "Built visually, tested before it runs.",
                body: "Lay out the trigger, the conditions and the actions on a canvas, top to bottom. Add AI steps to classify a reply or research a company, then press Test to dry-run the whole flow before it runs for real.",
                points: [
                    "If / else branches on anything in the event",
                    "Ask AI picks a branch from a plain question",
                    "Test dry-runs it on sample data; History shows every run",
                ],
                ask: "Explain what each of my automations does",
            },
        ],
    },
    {
        id: "remie",
        name: "Remie",
        slides: [
            {
                id: "ask",
                scene: "remieAsk",
                anchor: "remie",
                label: "Remie",
                lead: "Remie.",
                title: "Ask about anything in your workspace.",
                body: "Remie is the AI assistant on every page. Ask in plain words and it reads across your campaigns, mailboxes, contacts, inbox and CRM to answer, showing each step as it works.",
                points: [
                    "Open it from the blob in the top bar, or press ⌘I",
                    "It knows which page you are on",
                    "The panel follows you from page to page",
                ],
                remie: "That's me. I read the same data you see, so ask me anything you would otherwise go looking for.",
                ask: "Give me a summary of how this week went",
            },
            {
                id: "act",
                scene: "remieAct",
                anchor: "remie",
                label: "Remie",
                lead: "Remie.",
                title: "Then let it do the work, with your OK.",
                body: "Remie can do almost anything you can: build a campaign, clean a list, answer replies, change settings. It acts with your permissions and asks before it changes anything. Anything that sends mail always waits for you.",
                points: [
                    "Approve or skip each change it proposes",
                    "Admins can always allow the safe kinds of action",
                    "Each step uses AI credits from your plan",
                ],
                remie: "Whenever you'd rather not click through something, hand it to me. I'll show you exactly what I'm about to do first.",
            },
        ],
    },
    {
        id: "slack",
        name: "Slack",
        slides: [
            {
                id: "ask",
                scene: "slackAsk",
                anchor: "/app/integrations",
                label: "Integrations",
                lead: "Slack.",
                title: "Ask @Warmbly from any channel.",
                body: "Connect Slack and the same assistant answers there. Mention @Warmbly in a channel or message it directly. It works with the same tools and your permissions, and answers in the thread.",
                points: [
                    "Each thread is one conversation, no need to mention it again",
                    "Ask Warmbly about this, from any message's More actions menu",
                    "Approve or deny each change right in the thread",
                ],
                remie: "In Slack I go by @Warmbly. Same me, same answers.",
                slack: "@Warmbly how did outreach do this week?",
            },
            {
                id: "inbox",
                scene: "slackInbox",
                anchor: "/app/integrations",
                label: "Integrations",
                lead: "Inbox in Slack.",
                title: "Work your replies with the team.",
                body: "Pick an inbox channel and every reply lands there, one thread per conversation, with buttons to draft with AI, mark interest, assign and reply. Notifications go to the channels you choose.",
                points: [
                    "Reply from Slack, sent from the right mailbox",
                    "Talk a reply through in its thread before answering",
                    "Route each kind of notification to its own channel",
                ],
                remie: "Your team can work replies together right in Slack, and I'll draft the answers when you ask.",
                slack: "@Warmbly draft a reply with the benchmarks and offer Thursday",
            },
        ],
    },
    {
        id: "plans",
        name: "Plans",
        slides: [
            {
                id: "included",
                scene: "planIncluded",
                lead: "Plans.",
                title: "The whole product, on every plan.",
                body: "Everything this tour just showed you comes with every paid plan, Starter included. Plans differ in how much you send each day and how many AI credits you get, not in which parts of Warmbly you can use.",
                points: [],
                remie: "I come with every plan too. The plan only decides how many credits I have each month.",
                ask: "Which plan fits sending 1,000 emails a day?",
            },
            {
                id: "compare",
                scene: "planCompare",
                lead: "Compare.",
                title: "What changes as you grow.",
                body: "Each plan sends more mail and comes with more AI credits. Business and Enterprise also keep your sending apart from other customers.",
                points: [],
                remie: "Tell me how many mailboxes you have and how much you want to send, and I'll size the plan for you.",
                ask: "Which plan fits my mailboxes and how much I want to send?",
            },
            {
                id: "choose",
                scene: "plans",
                lead: "Last thing.",
                title: "Pick the plan that fits.",
                body: "Choose monthly or yearly. You can switch plans any time, and the change takes effect straight away.",
                points: [],
            },
        ],
    },
];

export interface FlatSlide extends TourSlide {
    chapter: TourChapter;
    /** Index of the chapter, and of the slide within it. */
    ci: number;
    si: number;
}

/** The tour in order, the same on every instance: it always ends on the plans. */
export const TOUR_SLIDES: FlatSlide[] = TOUR_CHAPTERS.flatMap((chapter, ci) => chapter.slides.map((slide, si) => ({ ...slide, chapter, ci, si })));
