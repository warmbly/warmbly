// Org picker — moved off the shadcn DropdownMenu onto the same
// PopoverMenu primitive every other dropdown in the dashboard uses
// (folders, sort, accounts, schedule, sort). One animation, one
// surface, one set of styles.
//
// Sits in the sidebar header above the nav. Trigger is a slim h-7
// row: 18px slate-900 monogram tile, current org name, chevron.
// Hover greys the row; the active org in the popover gets a single
// faint slate-100 background — no big check mark, no avatar inside
// each row.

import React from "react";
import { useNavigate } from "react-router-dom";
import { ChevronDownIcon, PlusIcon, Settings2Icon } from "lucide-react";
import { useAppStore } from "@/stores";
import { NewWorkspaceDialog } from "@/components/app/organizations/NewWorkspaceDialog";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuLabel,
    PopoverMenuSeparator,
    PopoverMenuTrigger,
} from "@/components/ui/popover-menu";

function initials(name: string): string {
    return name
        .split(" ")
        .filter(Boolean)
        .map((w) => w[0])
        .join("")
        .toUpperCase()
        .slice(0, 2);
}

export function OrgSwitcher() {
    const navigate = useNavigate();
    const organizations = useAppStore((s) => s.organizations);
    const currentOrganization = useAppStore((s) => s.currentOrganization);
    const switchOrganization = useAppStore((s) => s.switchOrganization);
    const [newOpen, setNewOpen] = React.useState(false);

    const name = currentOrganization?.name ?? "Workspace";

    return (
        <>
        <PopoverMenu align="start">
            <PopoverMenuTrigger asChild>
                <button
                    type="button"
                    className="group w-full flex items-center gap-2 px-1.5 h-7 rounded-md hover:bg-slate-200/60 transition-colors text-left"
                >
                    <span className="size-[18px] rounded bg-slate-900 flex items-center justify-center shrink-0 overflow-hidden">
                        {currentOrganization?.avatar_url || currentOrganization?.avatar ? (
                            <img
                                src={currentOrganization.avatar_url ?? currentOrganization.avatar}
                                alt=""
                                className="w-full h-full object-cover"
                            />
                        ) : (
                            <span className="text-[9px] font-bold text-white leading-none tracking-tight">
                                {initials(name)}
                            </span>
                        )}
                    </span>
                    <span className="text-[12.5px] font-medium text-slate-900 truncate flex-1 min-w-0">
                        {name}
                    </span>
                    <ChevronDownIcon className="w-3 h-3 text-slate-400 shrink-0 group-hover:text-slate-700 transition-colors" />
                </button>
            </PopoverMenuTrigger>

            <PopoverMenuContent minWidth={232}>
                <PopoverMenuLabel>Workspaces</PopoverMenuLabel>
                {organizations.length === 0 ? (
                    <div className="px-3 py-2 text-[11.5px] text-slate-400">
                        No workspaces yet.
                    </div>
                ) : (
                    organizations.map((org) => {
                        const avatar = org.avatar_url ?? org.avatar;
                        return (
                            <PopoverMenuItem
                                key={org.id}
                                onSelect={() => switchOrganization(org.id)}
                                selected={org.id === currentOrganization?.id}
                                icon={
                                    <span className="size-4 rounded bg-slate-900 flex items-center justify-center shrink-0 overflow-hidden">
                                        {avatar ? (
                                            <img
                                                src={avatar}
                                                alt=""
                                                className="w-full h-full object-cover"
                                            />
                                        ) : (
                                            <span className="text-[8px] font-bold text-white leading-none tracking-tight">
                                                {initials(org.name)}
                                            </span>
                                        )}
                                    </span>
                                }
                            >
                                {org.name}
                            </PopoverMenuItem>
                        );
                    })
                )}
                <PopoverMenuSeparator />
                <PopoverMenuItem
                    onSelect={() => setNewOpen(true)}
                    icon={<PlusIcon className="w-3 h-3" />}
                >
                    New workspace
                </PopoverMenuItem>
                <PopoverMenuItem
                    onSelect={() => navigate("/select-org")}
                    icon={<Settings2Icon className="w-3 h-3" />}
                >
                    Manage workspaces
                </PopoverMenuItem>
            </PopoverMenuContent>
        </PopoverMenu>
        <NewWorkspaceDialog open={newOpen} onClose={() => setNewOpen(false)} />
        </>
    );
}
