// Advisor: continuously-evaluated recommendations about deliverability,
// mailbox configuration, warmup, campaign performance, copy, and list hygiene.
// Findings are surfaced where the fix lives (the campaign, the mailbox, the
// deliverability page) rather than in a separate inbox.

export type AdvisorSeverity = "critical" | "high" | "medium" | "low";

export type AdvisorCategory =
    | "deliverability"
    | "mailbox"
    | "warmup"
    | "campaign"
    | "copy"
    | "list";

// The dashboard nav tab a finding belongs to. Drives which tab shows a badge.
export type AdvisorSurface =
    | "campaigns"
    | "emails"
    | "deliverability"
    | "contacts"
    | "analytics"
    | "settings";

export type AdvisorStatus = "open" | "snoozed" | "dismissed" | "applied" | "resolved";

// One line of the before/after shown in the fix drawer. Every one-click fix
// renders its full effect this way before the user confirms.
export interface AdvisorPreviewChange {
    field: string;
    from: string;
    to: string;
}

export interface AdvisorAction {
    tool: string;
    args: unknown;
    label: string;
    // True when autopilot is allowed to apply this one unattended: a bounded,
    // reversible settings change that only moves in the safe direction.
    auto?: boolean;
    preview?: AdvisorPreviewChange[];
    undo?: { tool: string; args: unknown };
}

// What an agent fix reports back.
export interface AdvisorAgentResult {
    finding_id: string;
    // False when the agent read the current state and decided nothing needed
    // changing, which leaves the finding open.
    applied: boolean;
    summary: string;
    // The tools it called, in order. The checkable part of the receipt.
    steps?: string[];
}

export interface AdvisorFinding {
    id: string;
    organization_id: string;
    detector_key: string;
    category: AdvisorCategory;
    severity: AdvisorSeverity;
    surface: AdvisorSurface;

    entity_type?: string;
    entity_id?: string;
    entity_label?: string;

    // The entity this finding belongs to when it differs from the subject: a
    // step's copy problem belongs to its campaign, and the campaign row is
    // where someone goes looking for it.
    parent_type?: string;
    parent_id?: string;

    status: AdvisorStatus;
    impact: number;

    title: string;
    // How this finding names itself when several of its kind are shown
    // together, with a {count} placeholder. Empty means it always stands alone.
    group_title?: string;
    detail: string;
    remedy: string;
    // The ordered manual steps, set only by checks with no one-click fix. When
    // empty the card falls back to the remedy prose.
    steps?: string[];
    // Whether an agent can resolve this by editing something the platform owns.
    // False for anything living outside it, like a DNS record, so those show
    // their steps rather than a button that cannot succeed.
    agent_fixable?: boolean;
    // False while the card carries the built-in copy rather than AI-written
    // copy. The card is fully usable either way.
    narrated: boolean;

    // The numbers the detector fired on, rendered as chips under the detail.
    evidence?: Record<string, unknown>;
    action?: AdvisorAction;

    first_seen_at: string;
    last_seen_at: string;
    snoozed_until?: string;
    applied_at?: string;
    applied_result?: string;
}

export interface AdvisorSurfaceCount {
    surface: AdvisorSurface;
    total: number;
    critical: number;
    high: number;
}

export interface AdvisorSummary {
    score: number;
    total: number;
    critical: number;
    high: number;
    medium: number;
    low: number;
    surfaces: AdvisorSurfaceCount[];
    last_run_at?: string;
}

export interface AdvisorSettings {
    organization_id: string;
    enabled: boolean;
    muted_categories: string[];
    muted_detectors: string[];
    min_severity: AdvisorSeverity;
    // Applies the auto-safe fixes without asking, as the member who switched
    // it on.
    autopilot: boolean;
    autopilot_actor_id?: string;
    updated_at: string;
}

export interface AdvisorFindingsQuery {
    surface?: AdvisorSurface;
    category?: AdvisorCategory;
    entityType?: string;
    entityId?: string;
    status?: AdvisorStatus[];
    limit?: number;
}

// Presentation tokens, kept beside the types so every advisor surface renders
// severity identically.
export const SEVERITY_LABEL: Record<AdvisorSeverity, string> = {
    critical: "Critical",
    high: "Needs attention",
    medium: "Worth fixing",
    low: "Suggestion",
};

// Tints rather than fills. A translucent wash of the severity colour lets
// whatever is underneath (a row hover, the drawer's frosted panel) show
// through, so a chip reads as a mark on the surface instead of a sticker
// sitting on top of it.
export const SEVERITY_CHIP: Record<AdvisorSeverity, string> = {
    critical: "bg-rose-500/10 text-rose-700 border-rose-500/20",
    high: "bg-orange-500/10 text-orange-700 border-orange-500/20",
    medium: "bg-amber-500/10 text-amber-700 border-amber-500/25",
    low: "bg-sky-500/10 text-sky-700 border-sky-500/20",
};

export const SEVERITY_DOT: Record<AdvisorSeverity, string> = {
    critical: "bg-rose-500",
    high: "bg-orange-500",
    medium: "bg-amber-500",
    low: "bg-sky-500",
};

export const SEVERITY_RANK: Record<AdvisorSeverity, number> = {
    critical: 4,
    high: 3,
    medium: 2,
    low: 1,
};

// Tone for the inline indicator that sits on the row the problem is about. It
// has to read as a status on someone else's row rather than a control of its
// own, so it stays borderless until hovered.
export const SEVERITY_ROW: Record<AdvisorSeverity, string> = {
    critical: "bg-rose-500/10 text-rose-700 hover:bg-rose-500/20",
    high: "bg-orange-500/10 text-orange-700 hover:bg-orange-500/20",
    medium: "bg-amber-500/10 text-amber-700 hover:bg-amber-500/25",
    low: "bg-sky-500/10 text-sky-700 hover:bg-sky-500/20",
};

// The one-word verdict the row indicator shows. Deliberately shorter than
// SEVERITY_LABEL: a table cell has room for a word, not a phrase.
export const SEVERITY_SHORT: Record<AdvisorSeverity, string> = {
    critical: "Urgent",
    high: "Fix",
    medium: "Tune",
    low: "Tip",
};

export const CATEGORY_LABEL: Record<AdvisorCategory, string> = {
    deliverability: "Deliverability",
    mailbox: "Mailbox",
    warmup: "Warmup",
    campaign: "Campaign",
    copy: "Copy",
    list: "List",
};

// A run of findings from the same check, collapsed into one card. One
// misconfiguration repeated across twenty mailboxes should read as one problem
// with twenty subjects, not twenty problems.
export interface AdvisorGroup {
    key: string;
    // lead is the most urgent member, and supplies the shared detail and remedy.
    lead: AdvisorFinding;
    members: AdvisorFinding[];
}

// groupFindings collapses runs of the same check into one entry. A check only
// collapses if it declared a group title, and only from three members up: two
// cards naming two specific mailboxes are more useful than one card saying
// "2 mailboxes".
export function groupFindings(findings: AdvisorFinding[], minSize = 3): AdvisorGroup[] {
    const byKey = new Map<string, AdvisorFinding[]>();
    for (const f of findings) {
        const list = byKey.get(f.detector_key);
        if (list) list.push(f);
        else byKey.set(f.detector_key, [f]);
    }

    const out: AdvisorGroup[] = [];
    for (const [key, members] of byKey) {
        const collapse = members.length >= minSize && Boolean(members[0].group_title);
        if (collapse) {
            out.push({ key, lead: members[0], members });
        } else {
            for (const f of members) out.push({ key: f.id, lead: f, members: [f] });
        }
    }

    // Re-sort: a collapsed group takes the urgency of its most urgent member,
    // so a run of criticals never sinks below a single medium.
    return out.sort((a, b) => {
        const rank = SEVERITY_RANK[b.lead.severity] - SEVERITY_RANK[a.lead.severity];
        if (rank !== 0) return rank;
        if (b.members.length !== a.members.length) return b.members.length - a.members.length;
        return b.lead.impact - a.lead.impact;
    });
}

// groupTitle renders a group's heading, falling back to the lead's own title
// for a check that never declared one.
export function groupTitle(group: AdvisorGroup): string {
    if (group.members.length < 2 || !group.lead.group_title) return group.lead.title;
    return group.lead.group_title.replace("{count}", String(group.members.length));
}

// resolutionSteps is the ordered how-to for a finding with no one-click fix.
// Empty means the remedy prose is the whole answer.
export function resolutionSteps(finding: AdvisorFinding): string[] {
    return finding.steps ?? [];
}

// findingLink is the way to the screen where a manual fix is actually made. A
// step's problem sends you to its campaign, since a step has no page of its own.
export function findingLink(finding: AdvisorFinding): { href: string; label: string } | null {
    if (finding.entity_type === "campaign" && finding.entity_id) {
        return { href: `/app/campaigns/${finding.entity_id}`, label: "Open the campaign" };
    }
    if (finding.entity_type === "step" && finding.parent_id) {
        return { href: `/app/campaigns/${finding.parent_id}`, label: "Open the campaign" };
    }
    if (finding.entity_type === "email_account" && finding.entity_id) {
        return { href: `/app/emails?mailbox=${finding.entity_id}`, label: "Open the mailbox" };
    }
    switch (finding.surface) {
        case "deliverability":
            return { href: "/app/deliverability", label: "Open deliverability" };
        case "contacts":
            return { href: "/app/contacts", label: "Open contacts" };
        case "settings":
            return { href: "/app/settings/workspace", label: "Open workspace settings" };
        default:
            return null;
    }
}

// worstSeverity is the tone a group of findings should take: the most urgent
// one present. A row with a critical and three suggestions is a critical row.
export function worstSeverity(findings: AdvisorFinding[]): AdvisorSeverity | null {
    let worst: AdvisorSeverity | null = null;
    for (const f of findings) {
        if (!worst || SEVERITY_RANK[f.severity] > SEVERITY_RANK[worst]) worst = f.severity;
    }
    return worst;
}

// sortFindings orders a list the way every advisor surface shows it: most
// urgent first, applied ones last so a just-fixed card does not jump to the top.
export function sortFindings(findings: AdvisorFinding[]): AdvisorFinding[] {
    return [...findings].sort((a, b) => {
        if ((a.status === "applied") !== (b.status === "applied")) {
            return a.status === "applied" ? 1 : -1;
        }
        const rank = SEVERITY_RANK[b.severity] - SEVERITY_RANK[a.severity];
        return rank !== 0 ? rank : b.impact - a.impact;
    });
}

// An advisor run sliced by the entity each finding is about, so a list page can
// fetch its surface once and hand every row its own advice.
export interface AdvisorEntityIndex {
    // get returns the findings attached to one entity, most urgent first.
    get(entityId?: string | null): AdvisorFinding[];
    // unattached are the findings no row can carry: workspace-wide settings, or
    // an entity that is not on this page. The page-level summary shows these.
    unattached: AdvisorFinding[];
    // total counts every finding in the run, attached or not.
    total: number;
}

const EMPTY: AdvisorFinding[] = [];

// indexByEntity buckets a run by subject AND by parent, because the row a
// finding belongs on is not always the row it is about: a step's copy problem
// has no row of its own and belongs on its campaign.
export function indexByEntity(findings: AdvisorFinding[]): AdvisorEntityIndex {
    const byEntity = new Map<string, AdvisorFinding[]>();
    const unattached: AdvisorFinding[] = [];

    const push = (id: string, f: AdvisorFinding) => {
        const list = byEntity.get(id);
        if (list) {
            // A finding whose parent is also its subject must not be listed twice.
            if (!list.some((x) => x.id === f.id)) list.push(f);
        } else {
            byEntity.set(id, [f]);
        }
    };

    for (const f of findings) {
        if (f.entity_id) push(f.entity_id, f);
        if (f.parent_id) push(f.parent_id, f);
        if (!f.entity_id && !f.parent_id) unattached.push(f);
    }

    for (const [id, list] of byEntity) byEntity.set(id, sortFindings(list));

    return {
        get: (entityId) => (entityId ? (byEntity.get(entityId) ?? EMPTY) : EMPTY),
        unattached: sortFindings(unattached),
        total: findings.length,
    };
}

// Evidence keys are snake_case machine names; this renders them as readable
// labels without needing a per-detector translation table.
export function evidenceLabel(key: string): string {
    return key
        .replace(/_percent$/, " (%)")
        .replace(/_/g, " ")
        .replace(/\b7d\b/, "last 7 days")
        .replace(/\b30d\b/, "last 30 days")
        .replace(/^./, (c) => c.toUpperCase());
}

export function evidenceValue(value: unknown): string {
    if (value === null || value === undefined) return "—";
    if (typeof value === "boolean") return value ? "yes" : "no";
    if (Array.isArray(value)) return value.map((v) => String(v)).join(", ");
    if (typeof value === "number") return Number.isInteger(value) ? String(value) : value.toFixed(2);
    return String(value);
}
