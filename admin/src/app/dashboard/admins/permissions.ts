// Helpers over the permission catalog from GET /admin/permissions.
//
// Presets mirror models.AdminRolePermissions by permission NAME so the bits
// come from the catalog the backend serves; the grant endpoint takes only a
// bitmask, so a preset is resolved here before it is sent. "super" is every
// live bit in the catalog (the backend's AllAdminPermissions also carries the
// retired bits, which IsSuperAdmin ignores).

import type { PermissionInfo } from "@/lib/api/client/admin/admins";

export type PresetId = "super" | "support" | "ops" | "analyst";

export const PRESETS: { id: PresetId; label: string; hint: string; names: string[] | "all" }[] = [
    { id: "super", label: "Super", hint: "Every bit, including granting admin access.", names: "all" },
    {
        id: "support",
        label: "Support",
        hint: "Read users, campaigns and workspaces; triage warmup bans and appeals.",
        names: ["view_users", "view_campaigns", "view_warmup_pool", "manage_warmup_bans", "review_appeals", "view_audit_logs", "view_organizations"],
    },
    {
        id: "ops",
        label: "Ops",
        hint: "Run the fleet: workers, analytics, rate limits.",
        names: ["view_workers", "manage_workers", "view_analytics", "view_audit_logs", "manage_rate_limits", "view_organizations"],
    },
    {
        id: "analyst",
        label: "Analyst",
        hint: "Read-only across users, campaigns, analytics and workspaces.",
        names: ["view_users", "view_campaigns", "view_analytics", "view_audit_logs", "view_organizations"],
    },
];

export function presetMask(preset: PresetId, catalog: PermissionInfo[]): number {
    const p = PRESETS.find((x) => x.id === preset);
    if (!p) return 0;
    if (p.names === "all") return catalog.reduce((m, c) => m | c.permission, 0);
    const byName = new Map(catalog.map((c) => [c.name, c.permission]));
    return p.names.reduce((m, n) => m | (byName.get(n) ?? 0), 0);
}

// Which preset a mask is exactly, if any, so the dialog can highlight it.
export function matchPreset(mask: number, catalog: PermissionInfo[]): PresetId | null {
    for (const p of PRESETS) {
        if (presetMask(p.id, catalog) === mask) return p.id;
    }
    return null;
}

export function groupByCategory(catalog: PermissionInfo[]): { category: string; items: PermissionInfo[] }[] {
    const out: { category: string; items: PermissionInfo[] }[] = [];
    for (const p of catalog) {
        let g = out.find((x) => x.category === p.category);
        if (!g) {
            g = { category: p.category, items: [] };
            out.push(g);
        }
        g.items.push(p);
    }
    return out;
}

export function hasBit(mask: number, bit: number): boolean {
    return (mask & bit) === bit;
}

export function humanize(name: string): string {
    return name.replace(/_/g, " ");
}
