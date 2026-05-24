// Plan catalogue — single source of truth for plan names, colors,
// limits and feature gates. Mirrors warmbly-web's pricing page so the
// dashboard never lies about what the user is paying for.
//
// The marketing site advertises four paid plans (Starter / Grow /
// Business / Enterprise). "Free" is kept here as a private label for
// the "no active subscription" state — never marketed, never sold.

export type PlanID = "free" | "starter" | "grow" | "business" | "enterprise";

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
    accent: "slate" | "green" | "orange" | "indigo" | "gradient";
    /** Whether this plan unlocks dedicated IPs. */
    dedicatedIps: boolean;
    /** Featured / "most popular" on the pricing page. */
    featured?: boolean;
}

export const PLAN_CATALOG: Record<PlanID, PlanDef> = {
    free: {
        id: "free",
        label: "Free",
        description: "No active subscription — try one out.",
        priceMonthly: 0,
        priceAnnual: 0,
        sendsPerDay: 0,
        bullets: ["Limited preview", "No active sending"],
        accent: "slate",
        dedicatedIps: false,
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
        dedicatedIps: false,
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
        dedicatedIps: false,
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
            "Dedicated IPs",
        ],
        accent: "indigo",
        dedicatedIps: true,
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
            "Dedicated IPs",
            "Dedicated support",
        ],
        accent: "gradient",
        dedicatedIps: true,
    },
};

export const PAID_PLANS: PlanID[] = ["starter", "grow", "business", "enterprise"];

export function planOrder(id: PlanID): number {
    return (["free", "starter", "grow", "business", "enterprise"] as PlanID[]).indexOf(id);
}

export function isAtLeast(actual: PlanID, required: PlanID): boolean {
    return planOrder(actual) >= planOrder(required);
}

export function getPlan(id: string | undefined | null): PlanDef {
    const norm = (id ?? "free").toLowerCase() as PlanID;
    return PLAN_CATALOG[norm] ?? PLAN_CATALOG.free;
}

/** Tailwind classes for the colored PlanPill / sidebar plan badge. */
export const PLAN_ACCENT_CLASSES: Record<PlanDef["accent"], {
    pill: string;
    dot: string;
    /** Used in the header so the active pill stays readable on the chrome. */
    header: string;
}> = {
    slate: {
        pill: "bg-slate-100 text-slate-600 border-slate-200",
        dot: "bg-slate-400",
        header: "bg-slate-100 text-slate-600 border-slate-200",
    },
    green: {
        pill: "bg-emerald-50 text-emerald-700 border-emerald-100",
        dot: "bg-emerald-500",
        header: "bg-emerald-50 text-emerald-700 border-emerald-100",
    },
    orange: {
        pill: "bg-amber-50 text-amber-700 border-amber-100",
        dot: "bg-amber-500",
        header: "bg-amber-50 text-amber-700 border-amber-100",
    },
    indigo: {
        pill: "bg-indigo-50 text-indigo-700 border-indigo-100",
        dot: "bg-indigo-500",
        header: "bg-indigo-50 text-indigo-700 border-indigo-100",
    },
    gradient: {
        pill: "bg-gradient-to-r from-blue-50 to-purple-50 text-blue-700 border-blue-100",
        dot: "bg-gradient-to-r from-blue-500 to-purple-500",
        header: "bg-gradient-to-r from-blue-50 to-purple-50 text-blue-700 border-blue-100",
    },
};
