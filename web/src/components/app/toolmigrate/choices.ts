// What the wizard holds between the scan and the start, and how it becomes the request.

import {
    TOOL_MIGRATION_KINDS,
    type ToolCredentials,
    type ToolMigration,
    type ToolMigrationInFlight,
    type ToolMigrationKind,
    type ToolMigrationOptions,
    type ToolMigrationTargetChoice,
    type ToolProviderInfo,
} from "@/lib/api/models/app/toolmigrate/ToolMigration";
import type { AgencyClient } from "@/lib/api/models/app/agency/Agency";
import { isFinishedCampaign, sourceName, workspaceKey } from "./labels";

export interface Choices {
    kinds: ToolMigrationKind[];
    /** Campaign ids left out; every other campaign comes over. */
    excluded: string[];
    inFlight: ToolMigrationInFlight;
    skipFinished: boolean;
    warmup: boolean;
}

export function defaultChoices(info: ToolProviderInfo | undefined): Choices {
    return {
        kinds: TOOL_MIGRATION_KINDS.filter((k) => info?.kinds.includes(k)),
        excluded: [],
        inFlight: "skip",
        skipFinished: true,
        warmup: true,
    };
}

/** Leads come over with their campaigns, so they cannot be picked alone. */
export function withKind(c: Choices, kind: ToolMigrationKind, on: boolean): Choices {
    let kinds = on ? [...new Set([...c.kinds, kind])] : c.kinds.filter((k) => k !== kind);
    if (kind === "campaigns" && !on) kinds = kinds.filter((k) => k !== "leads");
    return { ...c, kinds: TOOL_MIGRATION_KINDS.filter((k) => kinds.includes(k)) };
}

/** Every campaign the scan listed, across source workspaces. */
export function allCampaigns(m: ToolMigration) {
    return (m.inventory ?? []).flatMap((w) => w.inventory.campaign_summaries ?? []);
}

/** The campaigns that would come over with these choices. */
export function chosenCampaignIds(m: ToolMigration, c: Choices): string[] {
    return allCampaigns(m)
        .filter((s) => !c.excluded.includes(s.id) && !(c.skipFinished && isFinishedCampaign(s.status)))
        .map((s) => s.id);
}

export function toOptions(m: ToolMigration, c: Choices): ToolMigrationOptions {
    // Nothing left out sends no list, which brings every campaign including any the preview could not list.
    const campaignIds = c.kinds.includes("campaigns") && c.excluded.length > 0 ? chosenCampaignIds(m, c) : [];
    return {
        kinds: c.kinds,
        campaign_ids: campaignIds.length ? campaignIds : undefined,
        in_flight: c.inFlight,
        skip_finished: c.skipFinished,
        warmup: c.warmup,
    };
}

/** Why the preview cannot be left, or null. */
export function choicesIssue(m: ToolMigration, c: Choices): string | null {
    if (c.kinds.length === 0) return "Choose at least one thing to bring over.";
    if (c.kinds.includes("campaigns") && allCampaigns(m).length > 0 && chosenCampaignIds(m, c).length === 0) {
        return "No campaign is left to bring over. Pick one, or turn campaigns off.";
    }
    return null;
}

// Agency mapping: each source workspace goes to a new client, an existing client, or nowhere.
export type Destination = { mode: "new"; name: string } | { mode: "existing"; orgId: string } | { mode: "skip" };

export type Mapping = Record<string, Destination>;

/** A new client named after each source workspace, or the existing client already called that. */
export function defaultMapping(m: ToolMigration, clients: AgencyClient[], fallback: string): Mapping {
    const byName = new Map(clients.map((c) => [c.name.trim().toLowerCase(), c.organization_id]));
    const out: Mapping = {};
    for (const w of m.inventory ?? []) {
        const name = sourceName(w.workspace.name, fallback);
        const existing = byName.get(name.toLowerCase());
        out[workspaceKey(w.workspace)] = w.error
            ? { mode: "skip" }
            : existing
              ? { mode: "existing", orgId: existing }
              : { mode: "new", name };
    }
    return out;
}

export function mappingIssue(m: ToolMigration, mapping: Mapping, fallback: string): string | null {
    const rows = (m.inventory ?? []).map((w) => ({ w, d: mapping[workspaceKey(w.workspace)] ?? { mode: "skip" as const } }));
    if (!rows.some((r) => r.d.mode !== "skip")) return "Choose a client for at least one source workspace.";
    const seen = new Set<string>();
    for (const { w, d } of rows) {
        if (d.mode !== "new") continue;
        const name = d.name.trim();
        if (!name) return `Name the new client for ${sourceName(w.workspace.name, fallback)}.`;
        const k = name.toLowerCase();
        if (seen.has(k)) return `Two new clients would both be called ${name}.`;
        seen.add(k);
    }
    return null;
}

export function toTargets(m: ToolMigration, mapping: Mapping): ToolMigrationTargetChoice[] {
    return (m.inventory ?? []).map((w) => {
        const d = mapping[workspaceKey(w.workspace)] ?? { mode: "skip" };
        const base = { workspace_id: w.workspace.id, key: w.workspace.key ?? 0 };
        if (d.mode === "new") return { ...base, create_name: d.name.trim() };
        if (d.mode === "existing") return { ...base, organization_id: d.orgId };
        return { ...base, skip: true };
    });
}

/** Every required field of every key is filled. */
export function credentialsIssue(info: ToolProviderInfo, creds: ToolCredentials[]): string | null {
    for (let i = 0; i < creds.length; i++) {
        const missing = info.fields.find((f) => !f.optional && !(creds[i][f.key] ?? "").trim());
        if (missing) return creds.length > 1 ? `Fill in ${missing.label} for key ${i + 1}.` : `Fill in ${missing.label}.`;
    }
    return null;
}

export const emptyCredentials = (info: ToolProviderInfo | undefined): ToolCredentials =>
    Object.fromEntries((info?.fields ?? []).map((f) => [f.key, f.options?.length && !f.optional ? f.options[0] : ""]));

export const credentialsTyped = (creds: ToolCredentials[], info: ToolProviderInfo | undefined) =>
    creds.some((c) => (info?.fields ?? []).some((f) => !f.options?.length && (c[f.key] ?? "").trim() !== ""));
