import { DashboardImage } from "@/components/ui/dashboard-image";

// Workspace switcher in the header. Each agency the user is staff of comes
// first, opening the agency view, with its client workspaces under it; then
// the user's other workspaces. A search appears once the list is long.

import React from "react";
import { useNavigate } from "@tanstack/react-router";
import { ArrowLeftIcon, BuildingIcon, ChevronDownIcon, PlusIcon, Settings2Icon } from "lucide-react";
import toast from "react-hot-toast/headless";
import { useAppStore } from "@/stores";
import useSwitchOrganization from "@/lib/api/hooks/app/organizations/useSwitchOrganization";
import { rememberAgency, useAgencies } from "@/lib/api/hooks/app/agency/useAgency";
import { currentPortal } from "@/lib/portal";
import { NewWorkspaceDialog } from "@/components/app/organizations/NewWorkspaceDialog";
import { AgencyMark } from "@/components/app/agency/AgencyMark";
import { SearchInput } from "@/components/ui/field";
import { useWorkspaceAgency } from "@/hooks/useClientBrand";
import { groupWorkspaces, matchesQuery } from "@/lib/agencyView";
import type { Organization } from "@/stores/slices/organizationSlice";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuLabel,
    PopoverMenuSeparator,
    PopoverMenuTrigger,
} from "@/components/ui/popover-menu";
import { cn } from "@/lib/utils";

// Past this many rows the menu gets a search.
const SEARCH_FROM = 8;

function initials(name: string): string {
    return name
        .split(" ")
        .filter(Boolean)
        .map((w) => w[0])
        .join("")
        .toUpperCase()
        .slice(0, 2);
}

function OrgAvatar({ org, size }: { org: Pick<Organization, "name" | "avatar" | "avatar_url">; size: "sm" | "md" }) {
    const avatar = org.avatar_url ?? org.avatar;
    return (
        <span
            className={cn(
                "rounded-md ring-1 ring-slate-200 flex items-center justify-center shrink-0 overflow-hidden",
                size === "sm" ? "size-5" : "size-6",
                avatar ? (size === "sm" ? "bg-white dark:bg-transparent p-px" : "bg-white dark:bg-transparent p-0.5") : "bg-slate-900",
            )}
        >
            {avatar ? (
                <DashboardImage src={avatar} alt="" className={cn("w-full h-full object-cover", size === "sm" ? "rounded-[3px]" : "rounded-[4px]")} />
            ) : (
                <span className={cn("font-bold text-white leading-none tracking-tight", size === "sm" ? "text-[9px]" : "text-[10px]")}>{initials(org.name)}</span>
            )}
        </span>
    );
}

export function OrgSwitcher() {
    const navigate = useNavigate();
    const organizations = useAppStore((s) => s.organizations);
    const currentOrganization = useAppStore((s) => s.currentOrganization);
    const setCurrentOrganization = useAppStore((s) => s.setCurrentOrganization);
    const switchOrgMutation = useSwitchOrganization();
    const [newOpen, setNewOpen] = React.useState(false);
    const [query, setQuery] = React.useState("");
    const agencies = useAgencies().data?.data ?? [];
    const { agency: workspaceAgency, staff } = useWorkspaceAgency();
    const onPortal = currentPortal().portal;

    const name = currentOrganization?.name ?? "Workspace";
    const { groups, own } = groupWorkspaces(organizations, agencies);
    const rows = organizations.length + agencies.length;
    const searchable = rows >= SEARCH_FROM;
    const q = searchable ? query : "";
    // An agency stays listed while any of its clients match, so the match keeps its heading.
    const shownGroups = groups
        .map((g) => ({ ...g, clients: g.clients.filter((c) => matchesQuery(c.name, q)) }))
        .filter((g) => matchesQuery(g.agency.name, q) || g.clients.length > 0);
    const shownOwn = own.filter((o) => matchesQuery(o.name, q));

    // The server session moves first; the local pointer follows only once it has.
    const handleSwitch = async (orgId: string) => {
        if (orgId === currentOrganization?.id) return;
        try {
            await switchOrgMutation.mutateAsync(orgId);
            const next = organizations.find((o) => o.id === orgId);
            if (next) setCurrentOrganization(next);
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };

    const openAgency = (id: string) => {
        rememberAgency(id);
        void navigate({ to: "/app/agency" });
    };

    const workspaceItem = (org: Organization, indent: boolean) => (
        <PopoverMenuItem
            key={org.id}
            onSelect={() => handleSwitch(org.id)}
            disabled={switchOrgMutation.isPending}
            selected={org.id === currentOrganization?.id}
            icon={
                <span className={cn("flex", indent && "ml-3")}>
                    <OrgAvatar org={org} size="sm" />
                </span>
            }
            description={!indent && org.agency ? `Managed by ${org.agency.name}` : undefined}
        >
            {org.name}
        </PopoverMenuItem>
    );

    return (
        <>
        <PopoverMenu align="start" onOpenChange={(o) => !o && setQuery("")}>
            <PopoverMenuTrigger asChild>
                <button
                    type="button"
                    className="group w-full flex items-center gap-2 pl-1 pr-2 h-8 rounded-md hover:bg-slate-200/60 transition-colors text-left"
                >
                    {currentOrganization ? <OrgAvatar org={currentOrganization} size="md" /> : <OrgAvatar org={{ name }} size="md" />}
                    <span className="text-[12.5px] font-medium text-slate-900 truncate flex-1 min-w-0">
                        {name}
                    </span>
                    <ChevronDownIcon className="w-3.5 h-3.5 text-slate-400 shrink-0 group-hover:text-slate-700 transition-colors" />
                </button>
            </PopoverMenuTrigger>

            <PopoverMenuContent minWidth={256}>
                {workspaceAgency && !staff && (
                    <>
                        <div className="px-3 py-2 text-[11.5px] text-slate-500">
                            Managed by <span className="font-medium text-slate-700">{workspaceAgency.name}</span>
                        </div>
                        <PopoverMenuSeparator />
                    </>
                )}
                {workspaceAgency && staff && (
                    <>
                        <PopoverMenuItem onSelect={() => openAgency(workspaceAgency.id)} icon={<ArrowLeftIcon className="w-3 h-3" />}>
                            Back to {workspaceAgency.name}
                        </PopoverMenuItem>
                        <PopoverMenuSeparator />
                    </>
                )}
                {searchable && (
                    <div className="px-2 pt-1 pb-1.5">
                        <SearchInput value={query} onChange={setQuery} placeholder="Find a workspace or agency" autoFocus />
                    </div>
                )}
                {shownGroups.map((g) => (
                    <React.Fragment key={g.agency.id}>
                        <PopoverMenuItem
                            onSelect={() => openAgency(g.agency.id)}
                            icon={<AgencyMark name={g.agency.name} size="sm" />}
                            description={`Agency · ${g.agency.client_count} ${g.agency.client_count === 1 ? "client" : "clients"}`}
                        >
                            {g.agency.name}
                        </PopoverMenuItem>
                        {g.clients.map((c) => workspaceItem(c, true))}
                        <PopoverMenuSeparator />
                    </React.Fragment>
                ))}
                {(shownOwn.length > 0 || groups.length === 0) && (
                    <PopoverMenuLabel>{groups.length > 0 ? "Your workspaces" : "Workspaces"}</PopoverMenuLabel>
                )}
                {organizations.length === 0 ? (
                    <div className="px-3 py-2 text-[11.5px] text-slate-400">
                        No workspaces yet.
                    </div>
                ) : (
                    shownOwn.map((org) => workspaceItem(org, false))
                )}
                {q && shownGroups.length === 0 && shownOwn.length === 0 && (
                    <div className="px-3 py-2 text-[11.5px] text-slate-400">Nothing matches.</div>
                )}
                <PopoverMenuSeparator />
                {/* A portal domain only opens its agency's workspaces, so nothing new is made here. */}
                {agencies.length === 0 && !onPortal && (
                    <PopoverMenuItem onSelect={() => navigate({ to: "/app/agency" })} icon={<BuildingIcon className="w-3 h-3" />}>
                        Set up an agency
                    </PopoverMenuItem>
                )}
                {!onPortal && (
                    <PopoverMenuItem
                        onSelect={() => setNewOpen(true)}
                        icon={<PlusIcon className="w-3 h-3" />}
                    >
                        New workspace
                    </PopoverMenuItem>
                )}
                <PopoverMenuItem
                    onSelect={() => navigate({ to: "/select-org" })}
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
