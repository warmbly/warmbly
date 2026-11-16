// The bar over a client view: whose workspace this is, which role it is drawn for, and the way out.

import { useNavigate } from "@tanstack/react-router";
import { ArrowLeftIcon, ChevronDownIcon, EyeIcon, XIcon } from "lucide-react";
import { PopoverMenu, PopoverMenuContent, PopoverMenuItem, PopoverMenuSeparator, PopoverMenuTrigger } from "@/components/ui/popover-menu";
import useRoles from "@/lib/api/hooks/app/organizations/useRoles";
import { rememberAgency } from "@/lib/api/hooks/app/agency/useAgency";
import { ALL_PERMISSIONS } from "@/lib/permissions";
import { useAppStore } from "@/stores";

export default function ClientViewBar() {
    const view = useAppStore((s) => s.clientView);
    const org = useAppStore((s) => s.memberOrganization);
    const setClientView = useAppStore((s) => s.setClientView);
    const roles = useRoles();
    const navigate = useNavigate();
    if (!view || !org) return null;

    const pick = (roleId: string, roleName: string, permissions: number) => setClientView({ ...view, roleId, roleName, permissions });
    const back = view.returnTo ?? "/app/agency";
    const backToAgency = () => {
        setClientView(null);
        if (org.agency) rememberAgency(org.agency.id);
        void navigate({ to: back });
    };

    return (
        <div role="status" className="flex min-h-9 flex-wrap items-center gap-x-3 gap-y-1 bg-slate-900 px-4 py-1.5 text-[12.5px] text-white animate-in fade-in slide-in-from-top-1 duration-300 motion-reduce:animate-none">
            <EyeIcon className="size-3.5 shrink-0 text-sky-300" />
            <span className="min-w-0 truncate">
                Viewing <span className="font-medium">{org.name}</span> as a client
            </span>
            <PopoverMenu align="start">
                <PopoverMenuTrigger asChild>
                    <button
                        type="button"
                        className="inline-flex h-6 items-center gap-1 rounded-md bg-white/10 px-2 text-[12px] text-white transition-colors hover:bg-white/20"
                    >
                        {view.roleName || "Role"}
                        <ChevronDownIcon className="size-3 opacity-70" />
                    </button>
                </PopoverMenuTrigger>
                <PopoverMenuContent minWidth={230}>
                    <PopoverMenuItem
                        selected={view.roleId === "owner"}
                        onSelect={() => pick("owner", "Owner", ALL_PERMISSIONS)}
                        description="Everything in the workspace"
                    >
                        Owner
                    </PopoverMenuItem>
                    {(roles.data ?? []).length > 0 && <PopoverMenuSeparator />}
                    {(roles.data ?? []).map((r) => (
                        <PopoverMenuItem
                            key={r.id}
                            selected={view.roleId === r.id}
                            onSelect={() => pick(r.id, r.name, r.permissions)}
                            description={r.member_count === 1 ? "1 member" : `${r.member_count} members`}
                        >
                            {r.name}
                        </PopoverMenuItem>
                    ))}
                </PopoverMenuContent>
            </PopoverMenu>
            <span className="hidden text-white/50 lg:inline">Only what this role can do is shown. Your own access is unchanged.</span>
            <span className="ml-auto flex items-center gap-1">
                {org.agency && (
                    <button type="button" onClick={backToAgency} className="inline-flex h-6 items-center gap-1 rounded-md px-2 text-[12px] text-white/80 transition-colors hover:bg-white/10 hover:text-white">
                        <ArrowLeftIcon className="size-3" />
                        {back === "/app/agency/branding" ? "Branding" : "Agency"}
                    </button>
                )}
                <button
                    type="button"
                    onClick={() => setClientView(null)}
                    className="inline-flex h-6 items-center gap-1 rounded-md bg-white px-2 text-[12px] font-medium text-slate-900 transition-colors hover:bg-slate-100"
                >
                    <XIcon className="size-3" />
                    Exit client view
                </button>
            </span>
        </div>
    );
}
