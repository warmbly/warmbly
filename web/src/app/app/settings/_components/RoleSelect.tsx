// Shared workspace-role dropdown: colored dot, name, description, check on
// the active row. Roles are data (seeded Admin/Manager/Viewer + anything the
// workspace created); Owner is a membership status and never appears here.

import React from "react";
import { CheckIcon, ChevronDownIcon, Loader2Icon } from "lucide-react";
import type OrganizationRole from "@/lib/api/models/app/organizations/OrganizationRole";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuTrigger,
} from "@/components/ui/popover-menu";

const FALLBACK_COLOR = "#64748b";

export function roleColor(role?: Pick<OrganizationRole, "color"> | null) {
    return role?.color || FALLBACK_COLOR;
}

export default function RoleSelect({
    roles,
    value,
    fallbackLabel,
    onChange,
    pending = false,
    align = "start",
}: {
    roles: OrganizationRole[];
    /** Currently selected role id (member.role_id / draft selection). */
    value?: string | null;
    /** Shown when value matches no role (e.g. the role was deleted). */
    fallbackLabel?: string;
    onChange: (role: OrganizationRole) => void;
    pending?: boolean;
    align?: "start" | "end";
}) {
    const [open, setOpen] = React.useState(false);
    const current = roles.find((r) => r.id === value);
    const label = current?.name ?? fallbackLabel ?? "Select role";
    const color = current ? roleColor(current) : FALLBACK_COLOR;

    return (
        <PopoverMenu open={open} onOpenChange={setOpen} align={align}>
            <PopoverMenuTrigger asChild>
                <button
                    type="button"
                    disabled={pending || roles.length === 0}
                    className="h-6 px-1.5 rounded text-[10px] uppercase tracking-[0.08em] font-semibold inline-flex items-center gap-1 border border-slate-200 bg-white text-slate-700 hover:bg-slate-50 transition-colors disabled:opacity-60"
                >
                    {pending ? (
                        <Loader2Icon className="w-2.5 h-2.5 animate-spin" />
                    ) : (
                        <span className="size-1.5 rounded-full" style={{ backgroundColor: color }} />
                    )}
                    {label}
                    <ChevronDownIcon className="w-2.5 h-2.5 opacity-60" />
                </button>
            </PopoverMenuTrigger>
            <PopoverMenuContent minWidth={288} className="max-w-[calc(100vw-2rem)]">
                {roles.map((r) => {
                    const selected = r.id === value;
                    return (
                        <button
                            key={r.id}
                            type="button"
                            onClick={() => {
                                setOpen(false);
                                onChange(r);
                            }}
                            className={`w-full px-2.5 py-1.5 text-left hover:bg-slate-100 transition-colors ${
                                selected ? "bg-slate-50" : ""
                            }`}
                        >
                            <div className="flex items-center gap-2">
                                <span className="size-1.5 rounded-full" style={{ backgroundColor: roleColor(r) }} />
                                <span className="text-[12px] font-medium text-slate-900">{r.name}</span>
                                {selected && <CheckIcon className="ml-auto w-3 h-3 text-slate-500" />}
                            </div>
                            {r.description && (
                                <p className="text-[11px] text-slate-500 leading-tight mt-0.5">{r.description}</p>
                            )}
                        </button>
                    );
                })}
                {roles.length === 0 && (
                    <div className="px-2.5 py-2 text-[11.5px] text-slate-500">
                        No roles yet. Create one under Settings → Roles & access.
                    </div>
                )}
            </PopoverMenuContent>
        </PopoverMenu>
    );
}
