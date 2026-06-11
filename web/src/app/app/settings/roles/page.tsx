// Roles & access — workspace permission catalogue.
//
// Roles are data: every workspace starts with seeded Admin / Manager /
// Viewer rows that can be renamed, recolored, reshaped, or deleted like any
// other role. Owner is a membership status, not a role, and appears here
// only as a reference column.

import React from "react";
import { CheckIcon, LockIcon, XIcon } from "lucide-react";
import { Link } from "react-router-dom";
import useFeatureAccess from "@/hooks/useFeatureAccess";
import useRoles from "@/lib/api/hooks/app/organizations/useRoles";
import type OrganizationRole from "@/lib/api/models/app/organizations/OrganizationRole";
import { useAppStore } from "@/stores";
import {
    CATEGORY_LABEL,
    OWNER_DEF,
    PERMISSION_CATALOG,
    hasPermission,
    type PermissionDef,
} from "@/lib/permissions";
import { Section, SectionShell, TableSurface } from "../_components/SectionShell";
import { roleColor } from "../_components/RoleSelect";
import RolesSection from "./RolesSection";

export default function RolesSettingsPage() {
    const access = useFeatureAccess();
    const customRoles = useRoles();
    const currentOrg = useAppStore((s) => s.currentOrganization);

    if (!access.loading && !access.canManage) {
        return (
            <SectionShell title="Roles & access" description="Team managers only.">
                <Section eyebrow="Permission denied">
                    <div className="flex items-start gap-3">
                        <div className="size-9 rounded-md bg-amber-50 border border-amber-200 text-amber-700 flex items-center justify-center shrink-0">
                            <LockIcon className="w-4 h-4" />
                        </div>
                        <div>
                            <div className="text-[13px] font-semibold text-slate-900">
                                You need team management access to manage roles
                            </div>
                            <p className="text-[12px] text-slate-500 leading-relaxed mt-1 max-w-md">
                                Roles control who can do what inside this workspace. Ask someone
                                with team access to review or change your permissions.
                            </p>
                        </div>
                    </div>
                </Section>
            </SectionShell>
        );
    }

    const roles = customRoles.data ?? [];

    return (
        <SectionShell
            title="Roles & access"
            description={`What each role can do inside ${currentOrg?.name ?? "this workspace"}.`}
        >
            <Section
                eyebrow="Workspace roles"
                description="Every workspace starts with Admin, Manager, and Viewer. Rename, recolor, reshape, or delete them — and add your own. Editing a role updates everyone assigned to it."
            >
                <RolesSection canManage={access.canManage} />
                <p className="text-[11.5px] text-slate-500 leading-relaxed">
                    Assign roles from the member roster or the invite flow.{" "}
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
                description="Every capability and which role grants it. Owner is the workspace's ownership status, shown for reference."
            >
                <TableSurface>
                    <MatrixTable roles={roles} />
                </TableSurface>
            </Section>
        </SectionShell>
    );
}

function MatrixTable({ roles }: { roles: OrganizationRole[] }) {
    const cols: { id: string; label: string; color: string; permissions: number }[] = [
        { id: "owner", label: OWNER_DEF.label, color: OWNER_DEF.color, permissions: OWNER_DEF.permissions },
        ...roles.map((r) => ({ id: r.id, label: r.name, color: roleColor(r), permissions: r.permissions })),
    ];
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
                        {cols.map((r) => (
                            <th key={r.id} className="px-2 py-2 text-center min-w-[76px] md:min-w-[100px]">
                                <div className="flex items-center justify-center gap-1.5">
                                    <span
                                        className="size-1.5 rounded-full"
                                        style={{ backgroundColor: r.color }}
                                    />
                                    <span className="text-[10.5px] uppercase tracking-[0.1em] font-semibold text-slate-700 truncate max-w-[90px]">
                                        {r.label}
                                    </span>
                                </div>
                            </th>
                        ))}
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
                                                            className="inline-flex items-center justify-center size-5 rounded-full border"
                                                            style={{
                                                                backgroundColor: `${r.color}1a`,
                                                                borderColor: `${r.color}55`,
                                                                color: r.color,
                                                            }}
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
