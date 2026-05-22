// Org picker — sits in the top breadcrumb, styled for the sky chrome.
// Reads as a button that says "current org" and opens a dropdown to
// switch or create. The trigger is intentionally compact (no plan
// subtitle, no chevron-up-down) because it lives in a one-line
// breadcrumb, not a sidebar card.

import { ChevronDownIcon, PlusIcon } from "lucide-react";
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuLabel,
    DropdownMenuSeparator,
    DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useAppStore } from "@/stores";

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
    const organizations = useAppStore((s) => s.organizations);
    const currentOrganization = useAppStore((s) => s.currentOrganization);
    const switchOrganization = useAppStore((s) => s.switchOrganization);

    const name = currentOrganization?.name ?? "Warmbly";

    return (
        <DropdownMenu>
            <DropdownMenuTrigger asChild>
                <button className="flex items-center gap-2 px-2 h-7 rounded-md hover:bg-slate-200/60 transition-colors group cursor-pointer max-w-[14rem]">
                    <span className="w-[18px] h-[18px] rounded bg-sky-600 flex items-center justify-center shrink-0">
                        <span className="text-[9px] font-bold text-white leading-none">
                            {initials(name)}
                        </span>
                    </span>
                    <span className="text-[13px] font-medium text-slate-900 truncate">
                        {name}
                    </span>
                    <ChevronDownIcon className="w-3 h-3 text-slate-400 shrink-0" />
                </button>
            </DropdownMenuTrigger>
            <DropdownMenuContent className="min-w-56" align="start" sideOffset={6}>
                <DropdownMenuLabel className="text-xs text-zinc-400 font-normal">
                    Organizations
                </DropdownMenuLabel>
                {organizations.map((org) => (
                    <DropdownMenuItem
                        key={org.id}
                        onClick={() => switchOrganization(org.id)}
                        className={org.id === currentOrganization?.id ? "bg-zinc-50" : ""}
                    >
                        <div className="w-4 h-4 rounded bg-zinc-200 flex items-center justify-center shrink-0">
                            <span className="text-[8px] font-bold text-zinc-600">
                                {initials(org.name)}
                            </span>
                        </div>
                        <span className="ml-2 text-sm">{org.name}</span>
                    </DropdownMenuItem>
                ))}
                <DropdownMenuSeparator />
                <DropdownMenuItem>
                    <PlusIcon className="w-4 h-4" />
                    <span className="ml-2 text-sm">Create Organization</span>
                </DropdownMenuItem>
            </DropdownMenuContent>
        </DropdownMenu>
    );
}
