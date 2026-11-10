// Plan catalogue — single source of truth for plan names, colors,
// limits and feature gates. Mirrors warmbly-web's pricing page so the
// dashboard never lies about what the user is paying for.
//
// The marketing site advertises four paid plans (Starter / Grow /
// Business / Enterprise). "Free" is kept here as a private label for
// the "no active subscription" state — never marketed, never sold.
// "Warmup" is the $15 pool plan: the premium pool, with no mailbox cap, and
// nothing else, bought from the mailboxes page or a linked instance rather
// than from the plans grid. Its pitch lives in WARMUP_PLAN_* below.
// "Partnership" carries the Warmup plan's entitlements at a partner price and
// is sold only to a workspace Warmbly has offered it to.

export type PlanID = "free" | "warmup" | "partnership" | "starter" | "grow" | "business" | "enterprise";

export const TEST_PLAN_ID = "00000000-0000-0000-0000-0000000000e1";

export interface PlanDef {
    id: PlanID;
    label: string;
    /** Marketing tagline lifted from the pricing page. */
    description: string;
    /** USD monthly when paid monthly. null for custom / unlisted. */
    priceMonthly: number | null;
    /** USD monthly when billed annually (20% off). */
    priceAnnual: number | null;
    /** Hard daily send limit. Infinity for enterprise. */
    sendsPerDay: number;
    /** Marketing-page bullets shown in the upgrade card + billing page. */
    bullets: string[];
    /** Dashboard accent color for the PlanPill + sidebar badges. */
    accent: "slate" | "green" | "orange" | "sky" | "ink";
    /** Whether this plan's sending runs on infrastructure bound to this
     *  organization alone (plan.DedicatedWorkers > 0 server-side). What it
     *  buys is reputation isolation, not an IP-management product, so never
     *  describe it as a "dedicated IP". */
    isolatedSending: boolean;
    /** Featured / "most popular" on the pricing page. */
    featured?: boolean;
}

export const PLAN_CATALOG: Record<PlanID, PlanDef> = {
    free: {
        id: "free",
        label: "Free",
        description: "Look around the dashboard. Mailboxes and warmup start with a plan.",
        priceMonthly: 0,
        priceAnnual: 0,
        sendsPerDay: 0,
        bullets: ["Explore the dashboard", "No mailboxes or warmup", "No sending"],
        accent: "slate",
        isolatedSending: false,
    },
    warmup: {
        id: "warmup",
        label: "Warmup",
        description: "Better deliverability for every mailbox: the premium warmup pool, reputation protection and no mailbox cap.",
        priceMonthly: 15,
        priceAnnual: 12,
        sendsPerDay: 0,
        bullets: ["Better deliverability", "Reputation protected", "Every mailbox, no cap"],
        accent: "sky",
        isolatedSending: false,
    },
    partnership: {
        id: "partnership",
        label: "Partnership",
        description: "The premium warmup pool and no mailbox cap, offered by Warmbly to partners.",
        priceMonthly: 90,
        priceAnnual: null,
        sendsPerDay: 0,
        bullets: ["Premium warmup pool", "Every mailbox, no cap", "Offered by Warmbly"],
        accent: "sky",
        isolatedSending: false,
    },
    starter: {
        id: "starter",
        label: "Starter",
        description: "Great for small businesses with a small budget.",
        priceMonthly: 29,
        priceAnnual: 23,
        sendsPerDay: 150,
        bullets: ["Unlimited warmup", "Unlimited mailboxes", "150 emails / day"],
        accent: "green",
        isolatedSending: false,
    },
    grow: {
        id: "grow",
        label: "Grow",
        description: "Ideal for growing businesses scaling their outreach.",
        priceMonthly: 89,
        priceAnnual: 71,
        sendsPerDay: 3_000,
        bullets: ["Unlimited warmup", "Unlimited mailboxes", "3,000 emails / day"],
        accent: "orange",
        isolatedSending: false,
    },
    business: {
        id: "business",
        label: "Business",
        description: "For established teams that need higher limits and advanced features.",
        priceMonthly: 329,
        priceAnnual: 263,
        sendsPerDay: 15_000,
        bullets: [
            "Unlimited warmup",
            "Unlimited mailboxes",
            "15,000 emails / day",
            "Sending kept apart from other customers",
        ],
        accent: "sky",
        isolatedSending: true,
        featured: true,
    },
    enterprise: {
        id: "enterprise",
        label: "Enterprise",
        description: "Large orgs with custom volume and dedicated support.",
        priceMonthly: null,
        priceAnnual: null,
        sendsPerDay: Number.POSITIVE_INFINITY,
        bullets: [
            "Unlimited warmup",
            "Unlimited mailboxes",
            "15,000+ emails / day",
            "Sending kept apart from other customers",
            "Dedicated support",
        ],
        accent: "ink",
        isolatedSending: true,
    },
};

// The Warmup plan's server id (models.WarmupPlanID); matched by id so a renamed plan stays warmup-only.
export const WARMUP_PLAN_ID = "00000000-0000-0000-0000-000000000002";
// The Partnership plan's server id (models.PartnershipPlanID); warming only, like the Warmup plan.
export const PARTNERSHIP_PLAN_ID = "00000000-0000-0000-0000-000000000003";

// Agency plans reach a client workspace as a managed grant. Neither is sold from the plans grid, so each
// borrows the tier it matches for the dashboard's gates and keeps its own name on the plan pill.
export const AGENCY_PLAN_TIERS: Record<string, { tier: PlanID; label: string }> = {
    "00000000-0000-0000-0000-0000000000a1": { tier: "grow", label: "Agency" },
    "00000000-0000-0000-0000-0000000000a2": { tier: "business", label: "Agency Pro" },
};

/** Plans that pay for warming only; the server refuses sending to them like a free workspace. */
export function isWarmupOnlyPlan(id: PlanID): boolean {
    return id === "warmup" || id === "partnership";
}

export const PAID_PLANS: PlanID[] = ["starter", "grow", "business", "enterprise"];

// What the premium pool does for deliverability. Each line rests on a rule
// the pool enforces (priority matching, early quarantine, the borrowing
// floor in WarmupPartnerCandidates), never on a marketing number.
export const WARMUP_PLAN_PITCH = "Better deliverability for every mailbox you warm.";

export const WARMUP_PLAN_BENEFITS: { title: string; body: string }[] = [
    {
        title: "Better deliverability",
        body: "Your mailboxes warm in the premium pool, built for inbox placement, so more of your mail reaches the inbox.",
    },
    {
        title: "Reputation protected",
        body: "Mailboxes that start landing in spam are removed early, before they can affect yours.",
    },
    {
        title: "Priority matching",
        body: "Your mailboxes get their warmup partners before the free pool does.",
    },
    {
        title: "Every mailbox covered",
        body: "No 10 mailbox limit, here or on a linked instance.",
    },
];

export function planOrder(id: PlanID): number {
    // Partnership sits with Warmup: both warm only, so neither reaches a sending tier.
    if (id === "partnership") return 1;
    return (["free", "warmup", "starter", "grow", "business", "enterprise"] as PlanID[]).indexOf(id);
}

export function isAtLeast(actual: PlanID, required: PlanID): boolean {
    return planOrder(actual) >= planOrder(required);
}

// Server plan names that are not a catalog id. The pool plan was seeded as
// "Self-hosted pool" before it was renamed, and a deployment that has not run
// that migration still reports the old name.
const PLAN_ALIASES: Record<string, PlanID> = {
    "warmup pool": "warmup",
    "self-hosted pool": "warmup",
};

export function getPlan(id: string | undefined | null): PlanDef {
    const key = (id ?? "free").toLowerCase().trim();
    const norm = (PLAN_ALIASES[key] ?? key) as PlanID;
    return PLAN_CATALOG[norm] ?? PLAN_CATALOG.free;
}

/** Tailwind classes for the colored PlanPill / sidebar plan badge. */
export const PLAN_ACCENT_CLASSES: Record<PlanDef["accent"], {
    pill: string;
    dot: string;
    /** Used in the header so the active pill stays readable on the chrome. */
    header: string;
    /** Upgrade dialog: ring + glow on the highlighted plan card. */
    ring: string;
    /** Upgrade dialog: soft tinted wash behind the highlighted card. */
    soft: string;
    /** Upgrade dialog: primary CTA on that plan's card. */
    button: string;
}> = {
    slate: {
        pill: "bg-slate-100 text-slate-600 border-slate-200",
        dot: "bg-slate-400",
        header: "bg-slate-100 text-slate-600 border-slate-200",
        ring: "ring-slate-400 shadow-[0_24px_60px_-24px_rgba(100,116,139,0.45)]",
        soft: "from-slate-50",
        button: "bg-slate-900 hover:bg-slate-800 text-white",
    },
    green: {
        pill: "bg-emerald-50 text-emerald-700 border-emerald-100",
        dot: "bg-emerald-500",
        header: "bg-emerald-50 text-emerald-700 border-emerald-100",
        ring: "ring-emerald-500 shadow-[0_24px_60px_-24px_rgba(16,185,129,0.55)]",
        soft: "from-emerald-50",
        button: "bg-emerald-600 hover:bg-emerald-700 text-white",
    },
    orange: {
        pill: "bg-amber-50 text-amber-700 border-amber-100",
        dot: "bg-amber-500",
        header: "bg-amber-50 text-amber-700 border-amber-100",
        ring: "ring-amber-500 shadow-[0_24px_60px_-24px_rgba(245,158,11,0.55)]",
        soft: "from-amber-50",
        button: "bg-amber-600 hover:bg-amber-700 text-white",
    },
    sky: {
        pill: "bg-sky-50 text-sky-700 border-sky-100",
        dot: "bg-sky-500",
        header: "bg-sky-50 text-sky-700 border-sky-100",
        ring: "ring-sky-500 shadow-[0_24px_60px_-24px_rgba(14,165,233,0.4)]",
        soft: "from-sky-50",
        button: "bg-sky-600 hover:bg-sky-700 text-white",
    },
    ink: {
        pill: "bg-slate-900 text-white border-slate-900",
        dot: "bg-slate-900",
        header: "bg-slate-900 text-white border-slate-900",
        ring: "ring-slate-900 shadow-[0_24px_60px_-24px_rgba(15,23,42,0.45)]",
        soft: "from-slate-100",
        button: "bg-slate-900 hover:bg-slate-800 text-white",
    },
};
