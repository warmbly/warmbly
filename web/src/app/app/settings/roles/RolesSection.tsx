// Custom roles manager: built-in roles as read-only reference rows, custom
// roles with live member counts, and an editor that starts from a built-in
// preset and tweaks per-permission checkboxes. Edits propagate to assigned
// members server-side, so the roster refreshes alongside.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { Loader2Icon, PencilIcon, PlusIcon, Trash2Icon, XIcon } from "lucide-react";
import toast from "react-hot-toast/headless";
import { Label, TextInput } from "@/components/ui/field";
import { useConfirm } from "@/hooks/context/confirm";
import useRoles from "@/lib/api/hooks/app/organizations/useRoles";
import { useCreateRole, useDeleteRole, useUpdateRole } from "@/lib/api/hooks/app/organizations/useRoleMutations";
import type OrganizationRole from "@/lib/api/models/app/organizations/OrganizationRole";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import {
    CATEGORY_LABEL,
    PERMISSION_CATALOG,
    ROLE_TEMPLATES,
} from "@/lib/permissions";
import { roleColor } from "../_components/RoleSelect";
import { Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import { CLIENT_ROLE_PRESETS, roleScopeSafe, SCOPE_ACTION_PERMISSIONS, SCOPE_READ_PERMISSIONS } from "@/lib/accessScope";
import useEnsureClientRole from "@/lib/api/hooks/app/organizations/useEnsureClientRole";

// Ownership transfer can never live in a role (the API rejects it).
const EDITABLE_PERMISSIONS = PERMISSION_CATALOG.filter(
    (p) => p.key !== "TRANSFER_OWNERSHIP",
);
const CATEGORIES = ["data", "send", "people", "admin"] as const;

// Permission templates the editor can start from.
const PRESETS = ROLE_TEMPLATES;

// Swatch palette for role colors.
const COLORS = ["#0ea5e9", "#8b5cf6", "#10b981", "#f59e0b", "#f43f5e", "#06b6d4", "#84cc16", "#64748b"];

export default function RolesSection({ canManage }: { canManage: boolean }) {
    const roles = useRoles();
    const confirm = useConfirm();
    const deleteRole = useDeleteRole();
    const [editing, setEditing] = React.useState<OrganizationRole | "new" | null>(null);

    const customRoles = roles.data ?? [];

    function doDelete(role: OrganizationRole) {
        confirm?.show(`Delete the ${role.name} role?`, async () => {
            try {
                await toast.promise(deleteRole.mutateAsync(role.id), {
                    loading: "Deleting…",
                    success: "Role deleted",
                    error: (e: AppError) => buildError(e),
                });
            } catch {
                /* surfaced */
            }
        });
    }

    return (
        <div className="space-y-3">
            <div className="rounded-md border border-slate-200 bg-white divide-y divide-slate-200/60">
                {customRoles.map((role) => (
                    <div key={role.id} className="px-3 py-2 flex items-center gap-3">
                        <div className="min-w-0 flex-1">
                            <div className="flex items-center gap-2">
                                <span
                                    className="size-2 rounded-full shrink-0"
                                    style={{ backgroundColor: roleColor(role) }}
                                />
                                <span className="text-[12.5px] font-medium text-slate-900 truncate">{role.name}</span>
                                <ClientFit role={role} />
                            </div>
                            <p className="text-[11px] text-slate-500 truncate">
                                {role.description || `${countBits(role.permissions)} permissions`}
                            </p>
                        </div>
                        <span className="font-mono text-[10.5px] text-slate-400 tabular-nums shrink-0">
                            {role.member_count} {role.member_count === 1 ? "member" : "members"}
                        </span>
                        {canManage && (
                            <div className="flex items-center gap-1 shrink-0">
                                <button
                                    type="button"
                                    onClick={() => setEditing(role)}
                                    aria-label={`Edit ${role.name}`}
                                    className="size-6 rounded text-slate-400 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                                >
                                    <PencilIcon className="w-3 h-3" />
                                </button>
                                <button
                                    type="button"
                                    onClick={() => doDelete(role)}
                                    aria-label={`Delete ${role.name}`}
                                    className="size-6 rounded text-slate-400 hover:text-rose-600 hover:bg-rose-50 inline-flex items-center justify-center transition-colors"
                                >
                                    <Trash2Icon className="w-3 h-3" />
                                </button>
                            </div>
                        )}
                    </div>
                ))}
            </div>

            {canManage && <ClientPresets />}

            {canManage && (
                <button
                    type="button"
                    onClick={() => setEditing("new")}
                    className="h-7 px-2.5 rounded-md border border-slate-200 bg-white hover:bg-slate-50 text-[12px] font-medium text-slate-700 inline-flex items-center gap-1.5 transition-colors"
                >
                    <PlusIcon className="w-3.5 h-3.5" />
                    New role
                </button>
            )}

            <AnimatePresence>
                {editing && (
                    <RoleEditor
                        role={editing === "new" ? null : editing}
                        onClose={() => setEditing(null)}
                    />
                )}
            </AnimatePresence>
        </div>
    );
}

/** How a role reads for a client restricted to selected resources. */
function ClientFit({ role }: { role: OrganizationRole }) {
    const acts = !!role.acts_in_scope;
    const safe = roleScopeSafe(role.permissions, acts);
    if (!acts && !safe) return null;
    return (
        <span
            title={
                safe
                    ? "Every permission of this role applies to a client restricted to selected resources."
                    : "Clients holding this role act inside what is shared with them; its workspace-wide permissions do not apply to them."
            }
            className={`shrink-0 inline-flex items-center h-[18px] px-1.5 rounded text-[10.5px] font-medium ${
                safe ? "bg-sky-50 text-sky-700" : "bg-slate-100 text-slate-600"
            }`}
        >
            {acts ? "Clients can act" : "Client role"}
        </span>
    );
}

/** One-click client roles: each preset is added to the workspace as an ordinary role. */
function ClientPresets() {
    const clientRoles = useEnsureClientRole();
    const missing = CLIENT_ROLE_PRESETS.filter((p) => !clientRoles.roleFor(p));
    if (missing.length === 0) return null;
    async function addAll() {
        try {
            for (const p of missing) await clientRoles.ensure(p);
            toast.success(missing.length === 1 ? "Client role added" : "Client roles added");
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    }
    return (
        <div className="rounded-md border border-slate-200 bg-slate-50/60 p-3">
            <div className="flex items-center gap-2">
                <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Client roles</div>
                <button
                    type="button"
                    onClick={addAll}
                    disabled={clientRoles.pending}
                    className="ml-auto h-6 px-2 rounded-md border border-slate-200 bg-white text-[11.5px] font-medium text-slate-700 hover:border-slate-300 inline-flex items-center gap-1 disabled:opacity-60"
                >
                    {clientRoles.pending ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <PlusIcon className="w-3 h-3" />}
                    Add {missing.length === 1 ? "it" : `all ${missing.length}`}
                </button>
            </div>
            <p className="mt-1 text-[11.5px] text-slate-500 leading-relaxed">
                For clients restricted to selected campaigns and mailboxes. They work only inside what you share with them.
            </p>
            <ul className="mt-2 space-y-1">
                {missing.map((p) => (
                    <li key={p.key} className="flex items-start gap-2 text-[12px]">
                        <span className="mt-1 size-2 rounded-full shrink-0" style={{ backgroundColor: p.color }} />
                        <span>
                            <span className="font-medium text-slate-900">{p.name}</span>
                            <span className="text-slate-500">. {p.summary}</span>
                        </span>
                    </li>
                ))}
            </ul>
        </div>
    );
}

function countBits(mask: number) {
    let n = 0;
    for (let b = mask; b; b >>= 1) n += b & 1;
    return n;
}

function RoleEditor({ role, onClose }: { role: OrganizationRole | null; onClose: () => void }) {
    const create = useCreateRole();
    const update = useUpdateRole();
    const [name, setName] = React.useState(role?.name ?? "");
    const [description, setDescription] = React.useState(role?.description ?? "");
    const [color, setColor] = React.useState(role?.color || COLORS[0]);
    const [permissions, setPermissions] = React.useState<number>(
        role?.permissions ?? PRESETS.find((p) => p.id === "viewer")?.permissions ?? 0,
    );
    const [actsInScope, setActsInScope] = React.useState<boolean>(!!role?.acts_in_scope);
    const pending = create.isPending || update.isPending;
    const clientReach = permissions & (actsInScope ? SCOPE_ACTION_PERMISSIONS : SCOPE_READ_PERMISSIONS);

    const toggle = (bit: number) => setPermissions((p) => p ^ bit);

    async function save() {
        if (!name.trim()) {
            toast.error("Give the role a name");
            return;
        }
        try {
            await toast.promise(
                role
                    ? update.mutateAsync({ id: role.id, data: { name: name.trim(), description, color, permissions, acts_in_scope: actsInScope } })
                    : create.mutateAsync({ name: name.trim(), description, color, permissions, acts_in_scope: actsInScope }),
                {
                    loading: "Saving…",
                    success: role ? "Role updated" : "Role created",
                    error: (e: AppError) => buildError(e),
                },
            );
            onClose();
        } catch {
            /* surfaced */
        }
    }

    return (
        <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            className="fixed inset-0 z-50 bg-slate-900/30 flex items-center justify-center p-4"
            onMouseDown={(e) => {
                if (e.target === e.currentTarget) onClose();
            }}
        >
            <motion.div
                initial={{ opacity: 0, y: 8, scale: 0.98 }}
                animate={{ opacity: 1, y: 0, scale: 1 }}
                exit={{ opacity: 0, y: 8, scale: 0.98 }}
                className="w-full max-w-xl max-h-[85vh] overflow-y-auto rounded-lg bg-white border border-slate-200 shadow-xl"
            >
                <header className="px-4 h-12 flex items-center gap-2 border-b border-slate-200 sticky top-0 bg-white">
                    <h3 className="text-[13px] font-semibold text-slate-900">
                        {role ? `Edit ${role.name}` : "New role"}
                    </h3>
                    {role && role.member_count > 0 && (
                        <span className="text-[10.5px] text-amber-600">
                            Changes apply to {role.member_count} {role.member_count === 1 ? "member" : "members"} immediately
                        </span>
                    )}
                    <button
                        type="button"
                        onClick={onClose}
                        aria-label="Close"
                        className="ml-auto size-7 rounded-md text-slate-400 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center"
                    >
                        <XIcon className="w-4 h-4" />
                    </button>
                </header>

                <div className="p-4 space-y-4">
                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                        <div>
                            <Label>Name</Label>
                            <TextInput
                                value={name}
                                onChange={(v) => setName(v.slice(0, 50))}
                                placeholder="SDR"
                            />
                        </div>
                        <div>
                            <Label>Description</Label>
                            <TextInput
                                value={description}
                                onChange={setDescription}
                                placeholder="Replies in the inbox, no settings"
                            />
                        </div>
                    </div>

                    <div>
                        <Label>Start from</Label>
                        <div className="inline-flex items-center rounded-md border border-slate-200 bg-white p-0.5 flex-wrap">
                            {PRESETS.map((p) => (
                                <button
                                    key={p.id}
                                    type="button"
                                    onClick={() => {
                                        setPermissions(p.permissions);
                                        if (!role) setColor(p.color);
                                    }}
                                    className={`h-6 px-2.5 rounded text-[11.5px] font-medium transition-colors ${
                                        permissions === p.permissions
                                            ? "bg-slate-900 text-white"
                                            : "text-slate-500 hover:text-slate-900"
                                    }`}
                                >
                                    {p.label}
                                </button>
                            ))}
                        </div>
                        <p className="text-[11px] text-slate-400 mt-1">
                            Copies a template's permissions as a starting point, then tweak below.
                        </p>
                    </div>

                    <div>
                        <Label>Color</Label>
                        <div className="flex items-center gap-1.5">
                            {COLORS.map((c) => (
                                <button
                                    key={c}
                                    type="button"
                                    aria-label={`Use color ${c}`}
                                    onClick={() => setColor(c)}
                                    className={`size-6 rounded-full border-2 transition-transform ${
                                        color === c ? "border-slate-900 scale-110" : "border-transparent"
                                    }`}
                                    style={{ backgroundColor: c }}
                                />
                            ))}
                        </div>
                    </div>

                    <div className="rounded-md border border-slate-200 p-2.5">
                        <div className="flex items-center gap-2">
                            <span className="text-[12px] font-medium text-slate-900">Clients can act inside what is shared with them</span>
                            <span className="ml-auto">
                                <Toggle value={actsInScope} onChange={setActsInScope} ariaLabel="Clients can act inside what is shared with them" />
                            </span>
                        </div>
                        <p className="mt-1 text-[11px] text-slate-500 leading-relaxed">
                            For a member restricted to selected campaigns and mailboxes. Off, the role only lets them read.
                            On, they can also use this role's inbox, campaign editing and start or pause permissions on
                            what is shared with them. Team, billing, settings, contacts and mailbox settings never apply to them.
                        </p>
                        <p className="mt-1 text-[11px] text-slate-400">
                            {clientReach === 0
                                ? "A client with this role sees nothing."
                                : `A client with this role: ${EDITABLE_PERMISSIONS.filter((p) => (clientReach & p.bit) === p.bit).map((p) => p.label.toLowerCase()).join(", ")}.`}
                        </p>
                    </div>

                    {CATEGORIES.map((cat) => {
                        const perms = EDITABLE_PERMISSIONS.filter((p) => p.category === cat);
                        if (perms.length === 0) return null;
                        return (
                            <div key={cat}>
                                <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium mb-1.5">
                                    {CATEGORY_LABEL[cat].label}
                                </div>
                                <div className="grid grid-cols-1 sm:grid-cols-2 gap-1.5">
                                    {perms.map((p) => {
                                        const on = (permissions & p.bit) === p.bit;
                                        return (
                                            <button
                                                key={p.key}
                                                type="button"
                                                onClick={() => toggle(p.bit)}
                                                className={`px-2.5 py-1.5 rounded-md border text-left transition-colors ${
                                                    on
                                                        ? "border-sky-200 bg-sky-50"
                                                        : "border-slate-200 bg-white hover:bg-slate-50"
                                                }`}
                                            >
                                                <div className="flex items-center gap-2">
                                                    <span
                                                        className={`size-3.5 rounded-sm border inline-flex items-center justify-center shrink-0 ${
                                                            on ? "bg-sky-600 border-sky-600" : "border-slate-300 bg-white"
                                                        }`}
                                                    >
                                                        {on && (
                                                            <svg viewBox="0 0 10 10" className="w-2 h-2 text-white" fill="none">
                                                                <path d="M1.5 5.5L4 8L8.5 2.5" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
                                                            </svg>
                                                        )}
                                                    </span>
                                                    <span className="text-[12px] font-medium text-slate-900">{p.label}</span>
                                                </div>
                                                <p className="text-[10.5px] text-slate-500 mt-0.5 leading-tight">{p.description}</p>
                                            </button>
                                        );
                                    })}
                                </div>
                            </div>
                        );
                    })}
                </div>

                <footer className="px-4 h-12 flex items-center justify-end gap-2 border-t border-slate-200 sticky bottom-0 bg-white">
                    <button
                        type="button"
                        onClick={onClose}
                        className="h-7 px-2.5 rounded-md text-[12px] font-medium text-slate-600 hover:bg-slate-100 transition-colors"
                    >
                        Cancel
                    </button>
                    <button
                        type="button"
                        onClick={save}
                        disabled={pending}
                        className="h-7 px-3 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                    >
                        {pending && <Loader2Icon className="w-3 h-3 animate-spin" />}
                        {role ? "Save changes" : "Create role"}
                    </button>
                </footer>
            </motion.div>
        </motion.div>
    );
}
