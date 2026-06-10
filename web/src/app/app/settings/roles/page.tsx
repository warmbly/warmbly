// Roles & access — workspace permission catalogue.
//
// Structural page: shows the permission matrix and a summary of each
// role. The actual member roster + per-member role editing lives in
// the Members section so there's no duplication.

import React from "react";
import { CheckIcon, InfoIcon, LockIcon, XIcon } from "lucide-react";
import { Link } from "react-router-dom";
import useFeatureAccess from "@/hooks/useFeatureAccess";
import useMembers from "@/lib/api/hooks/app/organizations/useMembers";
import { useAppStore } from "@/stores";
import {
    CATEGORY_LABEL,
    PERMISSION_CATALOG,
    ROLE_CATALOG,
    hasPermission,
    type PermissionDef,
    type RoleDef,
} from "@/lib/permissions";
import { Section, SectionShell, TableSurface } from "../_components/SectionShell";

const ACCENT = {
    sky:     { dot: "bg-sky-500",     pill: "bg-sky-50 text-sky-700 border-sky-100" },
    violet:  { dot: "bg-violet-500",  pill: "bg-violet-50 text-violet-700 border-violet-100" },
    emerald: { dot: "bg-emerald-500", pill: "bg-emerald-50 text-emerald-700 border-emerald-100" },
    slate:   { dot: "bg-slate-400",   pill: "bg-slate-50 text-slate-700 border-slate-200" },
    amber:   { dot: "bg-amber-500",   pill: "bg-amber-50 text-amber-700 border-amber-100" },
} as const;

export default function RolesSettingsPage() {
    const access = useFeatureAccess();
    const members = useMembers();
    const currentOrg = useAppStore((s) => s.currentOrganization);

    if (!access.loading && !access.isOwner) {
        return (
            <SectionShell title="Roles & access" description="Owner only.">
                <Section eyebrow="Permission denied">
                    <div className="flex items-start gap-3">
                        <div className="size-9 rounded-md bg-amber-50 border border-amber-200 text-amber-700 flex items-center justify-center shrink-0">
                            <LockIcon className="w-4 h-4" />
                        </div>
                        <div>
                            <div className="text-[13px] font-semibold text-slate-900">
                                Only the workspace owner can manage roles
                            </div>
                            <p className="text-[12px] text-slate-500 leading-relaxed mt-1 max-w-md">
                                Roles control who can do what inside this workspace. Ask your owner
                                to review or change your permissions.
                            </p>
                        </div>
                    </div>
                </Section>
            </SectionShell>
        );
    }

    const memberList = members.data ?? [];
    const roleCounts = React.useMemo(() => {
        const out: Record<string, number> = {};
        for (const m of memberList) out[m.role] = (out[m.role] ?? 0) + 1;
        return out;
    }, [memberList]);

    return (
        <SectionShell
            title="Roles & access"
            description={`What each role can do inside ${currentOrg?.name ?? "this workspace"}.`}
        >
            <Section
                eyebrow="Built-in roles"
                description="Pick the closest match for each member. Custom permission bundles coming later."
            >
                <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
                    {ROLE_CATALOG.filter((r) => r.id !== "member").map((r) => (
                        <RoleSummaryCard
                            key={r.id}
                            role={r}
                            count={
                                (roleCounts[r.id] ?? 0) +
                                (r.id === "manager" ? roleCounts.member ?? 0 : 0)
                            }
                        />
                    ))}
                </div>
                <p className="text-[11.5px] text-slate-500 leading-relaxed">
                    Want to change a member's role?{" "}
                    <Link
                        to="/app/settings/members"
                        className="text-slate-700 underline-offset-2 hover:underline"
                    >
                        Open Members →
                    </Link>
                </p>
            </Section>

            <Section
                eyebrow="Permission matrix"
                description="Every capability and which role grants it."
            >
                <TableSurface>
                    <MatrixTable />
                </TableSurface>
            </Section>

            <Section eyebrow="Custom roles">
                <div className="flex items-start gap-3">
                    <div className="size-7 rounded-md bg-slate-100 text-slate-500 flex items-center justify-center shrink-0">
                        <InfoIcon className="w-3.5 h-3.5" />
                    </div>
                    <div className="text-[12px] text-slate-600 leading-relaxed">
                        Custom roles with bespoke permission bundles are coming soon. For now,
                        the four built-in roles cover the common patterns — pick the one closest
                        to what the member needs.
                    </div>
                </div>
            </Section>
        </SectionShell>
    );
}

function RoleSummaryCard({ role, count }: { role: RoleDef; count: number }) {
    const accent = ACCENT[role.accent as keyof typeof ACCENT];
    return (
        <div className="rounded-md border border-slate-200 bg-white px-3 py-2.5">
            <div className="flex items-center gap-1.5 mb-1">
                <span className={`size-1.5 rounded-full ${accent.dot}`} />
                <span className="text-[11px] uppercase tracking-[0.1em] font-semibold text-slate-700">
                    {role.label}
                </span>
                <span className="ml-auto font-mono text-[12px] text-slate-900 tabular-nums">
                    {count}
                </span>
            </div>
            <p className="text-[10.5px] text-slate-500 leading-snug">{role.description}</p>
        </div>
    );
}

function MatrixTable() {
    const cols = ROLE_CATALOG.filter((r) => r.id !== "member");
    const grouped = React.useMemo(() => {
        const out: Record<PermissionDef["category"], PermissionDef[]> = {
            data:   [],
            people: [],
            send:   [],
            admin:  [],
        };
        for (const p of PERMISSION_CATALOG) out[p.category].push(p);
        return out;
    }, []);

    return (
        <div className="overflow-x-auto">
            <table className="w-full text-left">
                <thead className="sticky top-0 z-10 bg-white">
                    <tr className="border-b border-slate-200">
                        <th className="px-4 py-2 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em] max-md:sticky max-md:left-0 max-md:z-[1] max-md:bg-white">
                            Capability
                        </th>
                        {cols.map((r) => {
                            const a = ACCENT[r.accent as keyof typeof ACCENT];
                            return (
                                <th
                                    key={r.id}
                                    className="px-2 py-2 text-center min-w-[76px] md:min-w-[100px]"
                                >
                                    <div className="flex items-center justify-center gap-1.5">
                                        <span className={`size-1.5 rounded-full ${a.dot}`} />
                                        <span className="text-[10.5px] uppercase tracking-[0.1em] font-semibold text-slate-700">
                                            {r.label}
                                        </span>
                                    </div>
                                </th>
                            );
                        })}
                    </tr>
                </thead>
                <tbody>
                    {(Object.entries(grouped) as [PermissionDef["category"], PermissionDef[]][]).map(
                        ([cat, perms]) => (
                            <React.Fragment key={cat}>
                                <tr className="bg-slate-50/60">
                                    <td
                                        colSpan={cols.length + 1}
                                        className="px-4 py-1.5 text-[10px] font-semibold uppercase tracking-[0.14em] text-slate-500"
                                    >
                                        <div className="max-md:sticky max-md:left-0 max-md:w-fit max-md:max-w-[calc(100vw-4rem)]">
                                            {CATEGORY_LABEL[cat].label}
                                            <span className="ml-2 normal-case tracking-normal text-slate-400 font-normal">
                                                {CATEGORY_LABEL[cat].description}
                                            </span>
                                        </div>
                                    </td>
                                </tr>
                                {perms.map((p) => (
                                    <tr
                                        key={p.key}
                                        className="border-b border-slate-200/60 last:border-0 hover:bg-slate-50/40 transition-colors"
                                    >
                                        <td className="px-4 py-2.5 align-top max-md:sticky max-md:left-0 max-md:z-[1] max-md:bg-white">
                                            <div className="text-[12px] text-slate-900 font-medium leading-tight max-md:max-w-[180px]">
                                                {p.label}
                                            </div>
                                            <div className="text-[11px] text-slate-500 leading-tight mt-0.5 max-md:max-w-[180px]">
                                                {p.description}
                                            </div>
                                        </td>
                                        {cols.map((r) => {
                                            const allowed = hasPermission(r.permissions, p.bit);
                                            return (
                                                <td key={r.id} className="px-2 py-2.5 text-center align-top">
                                                    {allowed ? (
                                                        <span
                                                            className={`inline-flex items-center justify-center size-5 rounded-full ${
                                                                ACCENT[r.accent as keyof typeof ACCENT].pill
                                                            } border`}
                                                            title="Granted"
                                                        >
                                                            <CheckIcon className="w-3 h-3" />
                                                        </span>
                                                    ) : (
                                                        <span
                                                            className="inline-flex items-center justify-center size-5 rounded-full text-slate-300"
                                                            title="Not granted"
                                                        >
                                                            <XIcon className="w-3 h-3" />
                                                        </span>
                                                    )}
                                                </td>
                                            );
                                        })}
                                    </tr>
                                ))}
                            </React.Fragment>
                        ),
                    )}
                </tbody>
            </table>
        </div>
    );
}
