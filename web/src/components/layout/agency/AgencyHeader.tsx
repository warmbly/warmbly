// The agency view's top row: the agency's mark and name (a switcher when the
// user is staff of more than one), the shared send pool and AI balance, the
// notification bell and the user menu. No workspace switcher or plan prompts.

import { Link, useNavigate } from "@tanstack/react-router";
import { ChevronDownIcon, LayoutGridIcon, LogOutIcon, Menu, SettingsIcon } from "lucide-react";
import { DashboardImage } from "@/components/ui/dashboard-image";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuLabel,
    PopoverMenuSeparator,
    PopoverMenuTrigger,
} from "@/components/ui/popover-menu";
import { ThemeSwitch } from "@/components/app/theme/ThemePicker";
import { AgencyBrandMark, AgencyMark } from "@/components/app/agency/AgencyMark";
import { formatNumber } from "@/components/app/agency/styles";
import { NotificationBell } from "../NotificationBell";
import { useAgencyAIBalance, useAgencyUsage, useCurrentAgency } from "@/lib/api/hooks/app/agency/useAgency";
import type { Agency } from "@/lib/api/models/app/agency/Agency";
import useLogout from "@/lib/api/hooks/auth/useLogout";
import { useAppStore } from "@/stores";
import { fmtUsd } from "@/lib/money";
import { cn } from "@/lib/utils";

const compact = new Intl.NumberFormat(undefined, { notation: "compact", maximumFractionDigits: 1 });

export function AgencyHeader({ agency, agencies, onMenu }: { agency?: Agency; agencies: Agency[]; onMenu?: () => void }) {
    return (
        <div className="h-14 flex items-center shrink-0">
            {onMenu && (
                <button
                    type="button"
                    onClick={onMenu}
                    aria-label="Open menu"
                    className="md:hidden ml-1.5 w-9 h-9 rounded-md flex items-center justify-center text-slate-600 hover:text-slate-900 hover:bg-slate-200/60 transition-colors shrink-0"
                >
                    <Menu className="w-5 h-5" />
                </button>
            )}
            <div className="h-full min-w-0 flex items-center pl-2 pr-3 md:w-64 md:px-4 md:shrink-0">
                {agency ? (
                    agencies.length > 1 ? (
                        <AgencySwitcher agency={agency} agencies={agencies} />
                    ) : (
                        <Link to="/app/agency" className="min-w-0 flex items-center gap-2.5 h-9 px-1 rounded-md hover:bg-slate-200/50 transition-colors">
                            <AgencyBrandMark agencyId={agency.id} name={agency.name} />
                            <AgencyTitle name={agency.name} />
                        </Link>
                    )
                ) : (
                    <span className="text-[13px] font-semibold text-slate-900">Agency</span>
                )}
            </div>
            <div className="flex-1 min-w-0" />
            <div className="flex items-center gap-1 sm:gap-2 px-2 sm:px-4 shrink-0">
                {agency && <HeaderMeters agencyId={agency.id} />}
                <NotificationBell />
                <AgencyUserMenu />
            </div>
        </div>
    );
}

function AgencyTitle({ name }: { name: string }) {
    return (
        <span className="min-w-0 flex flex-col leading-tight text-left">
            <span className="text-[13px] font-semibold text-slate-900 truncate">{name}</span>
            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Agency</span>
        </span>
    );
}

function AgencySwitcher({ agency, agencies }: { agency: Agency; agencies: Agency[] }) {
    const { select } = useCurrentAgency();
    const navigate = useNavigate();
    return (
        <PopoverMenu align="start">
            <PopoverMenuTrigger asChild>
                <button type="button" className="group min-w-0 w-full flex items-center gap-2.5 h-9 px-1 rounded-md hover:bg-slate-200/50 transition-colors">
                    <AgencyBrandMark agencyId={agency.id} name={agency.name} />
                    <AgencyTitle name={agency.name} />
                    <ChevronDownIcon className="ml-auto w-3.5 h-3.5 text-slate-400 group-hover:text-slate-700 shrink-0" />
                </button>
            </PopoverMenuTrigger>
            <PopoverMenuContent minWidth={240}>
                <PopoverMenuLabel>Your agencies</PopoverMenuLabel>
                {agencies.map((a) => (
                    <PopoverMenuItem
                        key={a.id}
                        selected={a.id === agency.id}
                        onSelect={() => {
                            select(a.id);
                            void navigate({ to: "/app/agency" });
                        }}
                        icon={<AgencyMark name={a.name} size="sm" />}
                        description={`${a.client_count} ${a.client_count === 1 ? "client" : "clients"}`}
                    >
                        {a.name}
                    </PopoverMenuItem>
                ))}
                <PopoverMenuSeparator />
                <PopoverMenuItem onSelect={() => navigate({ to: "/select-org" })} icon={<LayoutGridIcon className="w-3 h-3" />}>
                    All workspaces
                </PopoverMenuItem>
            </PopoverMenuContent>
        </PopoverMenu>
    );
}

// The shared send pool and AI balance at a glance; the usage page has the detail.
function HeaderMeters({ agencyId }: { agencyId: string }) {
    const usage = useAgencyUsage(agencyId).data;
    const ai = useAgencyAIBalance(agencyId).data;
    const share = usage && !usage.unlimited && usage.total > 0 ? Math.min(1, usage.used / usage.total) : null;
    const tone = share === null ? "" : share >= 1 ? "bg-red-500" : share >= 0.8 ? "bg-amber-500" : "bg-sky-600";
    const aiLow = !!ai && !ai.unmetered && ai.balance_usd <= 0;
    if (!usage && !ai) return null;
    return (
        <Link
            to="/app/agency/usage"
            className="hidden md:flex items-center gap-3 h-8 px-2.5 rounded-md hover:bg-slate-200/50 transition-colors"
            title="Send pool and AI balance this period"
        >
            {usage && (
                <span className="flex items-center gap-2">
                    <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Sends</span>
                    <span className="text-[12px] tabular-nums text-slate-800" title={`${formatNumber(usage.used)} of ${usage.unlimited ? "an uncapped pool" : formatNumber(usage.total)}`}>
                        {compact.format(usage.used)}
                        {!usage.unlimited && <span className="text-slate-400"> / {compact.format(usage.total)}</span>}
                    </span>
                    {share !== null && (
                        <span className="hidden lg:block w-14 h-1.5 rounded-full bg-slate-200 overflow-hidden">
                            <span className={cn("block h-full rounded-full", tone)} style={{ width: `${share * 100}%` }} />
                        </span>
                    )}
                </span>
            )}
            {ai && !ai.unmetered && (
                <>
                    <span className="h-4 w-px bg-slate-200" />
                    <span className="flex items-center gap-2">
                        <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">AI</span>
                        <span className={cn("text-[12px] tabular-nums", aiLow ? "text-red-600 font-medium" : "text-slate-800")}>{fmtUsd(ai.balance_usd)}</span>
                    </span>
                </>
            )}
        </Link>
    );
}

function AgencyUserMenu() {
    const navigate = useNavigate();
    const user = useAppStore((s) => s.user);
    const hasWorkspace = useAppStore((s) => !!s.currentOrganization);
    const logout = useLogout();
    if (!user) return null;
    const name = user.first_name && user.last_name ? `${user.first_name} ${user.last_name}` : user.email;

    const signOut = async () => {
        await logout.mutateAsync();
        void navigate({ to: "/auth/login", replace: true });
    };

    return (
        <PopoverMenu align="end">
            <PopoverMenuTrigger asChild>
                <button type="button" aria-label="Account menu" className="size-8 rounded-full flex items-center justify-center hover:bg-slate-200/60 transition-colors">
                    <span className="size-7 rounded-full bg-slate-900 flex items-center justify-center overflow-hidden">
                        {user.avatar_url ? (
                            <DashboardImage src={user.avatar_url} alt="" className="w-full h-full object-cover" />
                        ) : (
                            <span className="text-[11px] font-medium text-white leading-none">{user.email.slice(0, 2).toUpperCase()}</span>
                        )}
                    </span>
                </button>
            </PopoverMenuTrigger>
            <PopoverMenuContent minWidth={232}>
                <div className="px-3 py-2">
                    <div className="text-[12.5px] font-medium text-slate-900 truncate">{name}</div>
                    <div className="text-[11px] text-slate-400 truncate font-mono">{user.email}</div>
                </div>
                <PopoverMenuSeparator />
                <div className="flex h-8 items-center justify-between gap-3 pl-3 pr-1.5">
                    <span className="text-[12.5px] text-slate-700">Theme</span>
                    <ThemeSwitch />
                </div>
                <PopoverMenuSeparator />
                <PopoverMenuItem onSelect={() => navigate({ to: "/select-org" })} icon={<LayoutGridIcon className="w-3 h-3" />}>
                    All workspaces
                </PopoverMenuItem>
                {hasWorkspace && (
                    <PopoverMenuItem onSelect={() => navigate({ to: "/app/settings/profile" })} icon={<SettingsIcon className="w-3 h-3" />}>
                        Account settings
                    </PopoverMenuItem>
                )}
                <PopoverMenuSeparator />
                <PopoverMenuItem onSelect={() => void signOut()} icon={<LogOutIcon className="w-3 h-3" />} disabled={logout.isPending} danger>
                    {logout.isPending ? "Signing out…" : "Log out"}
                </PopoverMenuItem>
            </PopoverMenuContent>
        </PopoverMenu>
    );
}
