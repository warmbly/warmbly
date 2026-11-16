// "View as client": picks one of the agency's clients and opens it as that client's members see it.

import { ArrowUpRightIcon, EyeIcon, Loader2Icon } from "lucide-react";
import { PopoverMenu, PopoverMenuContent, PopoverMenuItem, PopoverMenuTrigger } from "@/components/ui/popover-menu";
import { ClientAvatar } from "@/components/app/agency/home/AgencyHome";
import { useAgencyOverview, useEnterClientView } from "@/lib/api/hooks/app/agency/useAgencyHome";
import type { ClientViewReturn } from "@/lib/clientView";

export function ViewAsClientMenu({
    agencyId,
    returnTo,
    beforeEnter,
    variant = "button",
}: {
    agencyId: string;
    returnTo?: ClientViewReturn;
    /** Runs first; false stays here (a save that failed). */
    beforeEnter?: () => Promise<boolean>;
    /** "tab" sits in a preview's tab strip: the real dashboard, where a drawn one would be. */
    variant?: "button" | "tab";
}) {
    const overview = useAgencyOverview(agencyId);
    const { enter, opening } = useEnterClientView({ returnTo });
    // Only a workspace the staff member belongs to can be opened.
    const clients = (overview.data?.clients ?? []).filter((c) => c.access !== "none").sort((a, b) => a.name.localeCompare(b.name));
    const disabled = clients.length === 0;
    const go = async (orgId: string) => {
        if (beforeEnter && !(await beforeEnter())) return;
        await enter(orgId);
    };
    return (
        <PopoverMenu align="end">
            <PopoverMenuTrigger asChild>
                {variant === "tab" ? (
                    <button
                        type="button"
                        disabled={disabled}
                        title={disabled ? "Add a client you are a member of first." : "Open a client's real dashboard, as its members see it, with your saved brand"}
                        className="h-7 shrink-0 rounded-md px-2.5 inline-flex items-center gap-1 text-[12.5px] whitespace-nowrap text-slate-600 outline-none transition-colors hover:bg-slate-100 hover:text-slate-900 focus-visible:ring-2 focus-visible:ring-sky-200 disabled:opacity-50 disabled:cursor-not-allowed"
                    >
                        Dashboard
                        {opening ? <Loader2Icon className="size-3 animate-spin" /> : <ArrowUpRightIcon className="size-3 text-slate-400" />}
                    </button>
                ) : (
                    <button
                        type="button"
                        disabled={disabled}
                        title={disabled ? "Add a client you are a member of first." : undefined}
                        className="h-7 px-2.5 rounded-md inline-flex items-center gap-1.5 text-[12px] font-medium transition-colors border border-slate-200 hover:border-slate-300 text-slate-700 hover:text-slate-900 bg-white disabled:opacity-60 disabled:cursor-not-allowed"
                    >
                        {opening ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <EyeIcon className="w-3 h-3" />}
                        View as client
                    </button>
                )}
            </PopoverMenuTrigger>
            <PopoverMenuContent minWidth={230}>
                {clients.map((c) => (
                    <PopoverMenuItem
                        key={c.organization_id}
                        disabled={opening !== null}
                        icon={<ClientAvatar name={c.name} avatar={c.avatar_url} size="sm" />}
                        description={variant === "tab" ? "Opens as its members see it" : undefined}
                        onSelect={() => void go(c.organization_id)}
                    >
                        {c.name}
                    </PopoverMenuItem>
                ))}
            </PopoverMenuContent>
        </PopoverMenu>
    );
}
