// Labels, tones and helpers shared by the migration list and wizard.

import type {
    ToolCampaignStatus,
    ToolMigration,
    ToolMigrationCounts,
    ToolMigrationInFlight,
    ToolMigrationKind,
    ToolMigrationStatus,
    ToolMigrationTargetStatus,
    ToolProviderInfo,
} from "@/lib/api/models/app/toolmigrate/ToolMigration";

export const KIND_LABEL: Record<ToolMigrationKind, string> = {
    blocklist: "Blocklist",
    mailboxes: "Mailboxes",
    campaigns: "Campaigns",
    leads: "Leads",
};

export function kindHint(kind: ToolMigrationKind, info?: ToolProviderInfo): string {
    switch (kind) {
        case "blocklist":
            return "Blocked addresses and domains join your suppression list first, so nobody who opted out is enrolled.";
        case "mailboxes":
            return info?.mailbox_secrets
                ? "SMTP and IMAP mailboxes connect with their saved passwords. Google and Microsoft ones need signing in again."
                : "Listed for reconnecting. Each owner signs in again here, or you send them a connect link.";
        case "campaigns":
            return "Steps, A/B variants, schedule and settings, created as drafts.";
        case "leads":
            return "Contacts with their custom fields, enrolled by where they stand in each campaign.";
    }
}

export const IN_FLIGHT: { value: ToolMigrationInFlight; label: string; hint: string }[] = [
    { value: "skip", label: "Import without enrolling", hint: "Adds them as contacts and leaves them out of the campaign." },
    { value: "restart", label: "Restart from step one", hint: "Sends the whole sequence again from the first step." },
];

export const STATUS: Record<ToolMigrationStatus, { label: string; tone: string }> = {
    scanning: { label: "Scanning", tone: "bg-sky-50 text-sky-700" },
    ready: { label: "Ready to start", tone: "bg-amber-50 text-amber-700" },
    running: { label: "Running", tone: "bg-sky-50 text-sky-700" },
    completed: { label: "Completed", tone: "bg-emerald-50 text-emerald-700" },
    failed: { label: "Failed", tone: "bg-red-50 text-red-700" },
    canceled: { label: "Canceled", tone: "bg-slate-100 text-slate-500" },
};

export const TARGET_STATUS: Record<ToolMigrationTargetStatus, { label: string; tone: string }> = {
    pending: { label: "Waiting", tone: "bg-slate-100 text-slate-500" },
    running: { label: "Running", tone: "bg-sky-50 text-sky-700" },
    completed: { label: "Done", tone: "bg-emerald-50 text-emerald-700" },
    failed: { label: "Failed", tone: "bg-red-50 text-red-700" },
    canceled: { label: "Canceled", tone: "bg-slate-100 text-slate-500" },
};

export const CAMPAIGN_STATUS: Record<ToolCampaignStatus, string> = {
    draft: "Draft",
    active: "Active",
    paused: "Paused",
    completed: "Completed",
    archived: "Archived",
};

export const isFinishedCampaign = (s: ToolCampaignStatus) => s === "completed" || s === "archived";

/** A count the source could not give cheaply is -1. */
export function fmtCount(n: number | undefined): string {
    if (n === undefined || n < 0) return "?";
    return n.toLocaleString();
}

export const plural = (n: number, one: string, many = `${one}s`) => `${n.toLocaleString()} ${n === 1 ? one : many}`;

const ZERO: ToolMigrationCounts = {
    blocklist_imported: 0,
    mailboxes_queued: 0,
    mailboxes_manual: 0,
    campaigns_created: 0,
    campaigns_skipped: 0,
    steps_created: 0,
    leads_read: 0,
    contacts_imported: 0,
    contacts_updated: 0,
    leads_enrolled: 0,
    leads_not_enrolled: 0,
    leads_suppressed: 0,
    leads_failed: 0,
};

/** Every target's counts added together. */
export function totalCounts(m: ToolMigration): ToolMigrationCounts {
    const out = { ...ZERO };
    for (const t of m.targets ?? []) {
        for (const k of Object.keys(out) as (keyof ToolMigrationCounts)[]) out[k] += t.counts?.[k] ?? 0;
    }
    return out;
}

/** How many things still wait on a person: mailboxes to reconnect and campaigns missing senders. */
export function followupCount(m: ToolMigration): number {
    let n = 0;
    for (const t of m.targets ?? []) {
        n += t.followup?.reconnect?.length ?? 0;
        n += (t.followup?.campaigns ?? []).filter((c) => (c.missing_senders?.length ?? 0) > 0).length;
    }
    return n;
}

/** One line saying what a migration brought over. */
export function countsSummary(m: ToolMigration): string {
    const c = totalCounts(m);
    const parts: string[] = [];
    if (c.campaigns_created) parts.push(plural(c.campaigns_created, "campaign"));
    if (c.contacts_imported + c.contacts_updated) parts.push(plural(c.contacts_imported + c.contacts_updated, "contact"));
    if (c.mailboxes_queued + c.mailboxes_manual) parts.push(plural(c.mailboxes_queued + c.mailboxes_manual, "mailbox", "mailboxes"));
    if (c.blocklist_imported) parts.push(`${c.blocklist_imported.toLocaleString()} blocked`);
    return parts.join(", ");
}

/** A heading for one source workspace: its name, or the account's own when the tool has none. */
export function sourceName(name: string | undefined, fallback: string): string {
    return name?.trim() || fallback;
}

/** The stable key for one source workspace: which pasted key read it, and its id there. */
export const workspaceKey = (ws: { id: string; key?: number }) => `${ws.key ?? 0}:${ws.id}`;
