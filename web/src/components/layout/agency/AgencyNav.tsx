// The agency view's sidebar: the agency's own pages, then every client
// workspace with a health dot from the overview. A client row enters that
// workspace. Below md it is an off-canvas drawer, as in the workspace shell.

import React from "react";
import { Link, useLocation } from "@tanstack/react-router";
import {
    Building2Icon,
    CreditCardIcon,
    GaugeIcon,
    InboxIcon,
    KeyRoundIcon,
    LayoutDashboardIcon,
    LayoutGridIcon,
    Loader2Icon,
    PaletteIcon,
    PlusIcon,
    UsersIcon,
    XIcon,
    type LucideIcon,
} from "lucide-react";
import { AgencyBrandMark } from "@/components/app/agency/AgencyMark";
import { AddClientDialog } from "@/components/app/agency/AddClientDialog";
import { ClientAvatar } from "@/components/app/agency/home/AgencyHome";
import { useAgencyClients } from "@/lib/api/hooks/app/agency/useAgency";
import { useAgencyOverview, useOpenClient } from "@/lib/api/hooks/app/agency/useAgencyHome";
import type { Agency } from "@/lib/api/models/app/agency/Agency";
import type { AgencyClientAccess } from "@/lib/api/models/app/agency/AgencyHome";
import { useAppStore } from "@/stores";
import { cn } from "@/lib/utils";

type ItemPath =
    | "/app/agency"
    | "/app/agency/clients"
    | "/app/agency/inbox"
    | "/app/agency/usage"
    | "/app/agency/billing"
    | "/app/agency/staff"
    | "/app/agency/branding"
    | "/app/agency/api-keys";

const ITEMS: { to: ItemPath; label: string; icon: LucideIcon; exact?: boolean; badge?: "replies" }[] = [
    { to: "/app/agency", label: "Overview", icon: LayoutDashboardIcon, exact: true },
    { to: "/app/agency/clients", label: "Clients", icon: Building2Icon },
    { to: "/app/agency/inbox", label: "Inbox", icon: InboxIcon, badge: "replies" },
    { to: "/app/agency/usage", label: "Usage", icon: GaugeIcon },
    { to: "/app/agency/billing", label: "Billing", icon: CreditCardIcon },
    { to: "/app/agency/staff", label: "Staff", icon: UsersIcon },
    { to: "/app/agency/branding", label: "Branding", icon: PaletteIcon },
    { to: "/app/agency/api-keys", label: "API keys", icon: KeyRoundIcon },
];

type Health = "critical" | "warning" | "ok";

interface ClientEntry {
    id: string;
    name: string;
    avatar?: string;
    access: AgencyClientAccess;
    health?: Health;
}

const ROW = "mx-3 w-[calc(100%-1.5rem)] h-7 px-[9px] gap-2.5 flex items-center rounded-md text-[12.5px] transition-colors";

export function AgencyNav({ agency, open, onClose }: { agency: Agency; open: boolean; onClose: () => void }) {
    const pathname = useLocation({ select: (l) => l.pathname.replace(/\/+$/, "") });
    const overview = useAgencyOverview(agency.id).data;
    const clients = useAgencyClients(agency.id);
    const currentOrgId = useAppStore((s) => s.currentOrganization?.id);
    const { open: enter, pending } = useOpenClient();
    const [entering, setEntering] = React.useState<string | null>(null);
    const [adding, setAdding] = React.useState(false);
    const canManage = agency.role === "owner" || agency.role === "admin";

    const entries = React.useMemo<ClientEntry[]>(() => {
        if (overview) {
            const health = new Map<string, Health>();
            for (const a of overview.attention) {
                if (!a.organization_id) continue;
                if (a.severity === "critical") health.set(a.organization_id, "critical");
                else if (!health.has(a.organization_id)) health.set(a.organization_id, "warning");
            }
            return overview.clients
                .map((c) => ({ id: c.organization_id, name: c.name, avatar: c.avatar_url, access: c.access, health: health.get(c.organization_id) ?? "ok" }))
                .sort((a, b) => a.name.localeCompare(b.name));
        }
        return (clients.data ?? [])
            .map((c) => ({ id: c.organization_id, name: c.name, avatar: c.avatar_url, access: "full" as const }))
            .sort((a, b) => a.name.localeCompare(b.name));
    }, [overview, clients.data]);

    const replies = overview?.totals.replies_waiting ?? 0;

    const openClient = async (id: string) => {
        setEntering(id);
        await enter(id);
        setEntering(null);
    };

    return (
        <>
            <div
                aria-hidden
                onClick={onClose}
                className={cn(
                    "fixed inset-0 z-40 bg-slate-900/40 transition-opacity duration-300 md:hidden",
                    open ? "opacity-100" : "pointer-events-none opacity-0",
                )}
            />
            <aside
                aria-label="Agency"
                className={cn(
                    "fixed inset-y-0 left-0 z-50 w-64 flex flex-col text-slate-900 bg-white shadow-2xl transition-transform duration-300 ease-out",
                    open ? "translate-x-0" : "-translate-x-full",
                    "md:static md:z-auto md:translate-x-0 md:bg-transparent md:shadow-none md:transition-none shrink-0",
                )}
            >
                <div className="md:hidden flex items-center justify-between gap-2 px-3 h-14 border-b border-slate-200/70">
                    <Link to="/app/agency" onClick={onClose} className="min-w-0 flex items-center gap-2.5">
                        <AgencyBrandMark agencyId={agency.id} name={agency.name} size="sm" />
                        <span className="text-[13px] font-semibold text-slate-900 truncate">{agency.name}</span>
                    </Link>
                    <button
                        type="button"
                        onClick={onClose}
                        aria-label="Close menu"
                        className="w-8 h-8 -mr-1 rounded-md flex items-center justify-center text-slate-500 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                    >
                        <XIcon className="w-4 h-4" />
                    </button>
                </div>

                <nav className="flex-1 overflow-y-auto overflow-x-hidden py-2">
                    <div className="space-y-px">
                        {ITEMS.map((item) => {
                            const active = item.exact ? pathname === item.to : pathname === item.to || pathname.startsWith(item.to + "/");
                            const Icon = item.icon;
                            return (
                                <Link
                                    key={item.to}
                                    to={item.to}
                                    aria-current={active ? "page" : undefined}
                                    className={cn(
                                        ROW,
                                        active ? "wb-nav-active bg-slate-200/70 text-slate-900 font-medium" : "text-slate-600 hover:text-slate-900 hover:bg-slate-200/40",
                                    )}
                                >
                                    <Icon className={cn("w-[14px] h-[14px] shrink-0", active ? "text-slate-700" : "text-slate-400")} strokeWidth={active ? 2 : 1.6} />
                                    <span className="flex-1 min-w-0 truncate">{item.label}</span>
                                    {item.badge === "replies" && replies > 0 && (
                                        <span className="min-w-4 h-4 px-1 rounded-full bg-sky-600 text-white text-[10px] font-medium tabular-nums inline-flex items-center justify-center">
                                            {replies > 99 ? "99+" : replies}
                                        </span>
                                    )}
                                </Link>
                            );
                        })}
                    </div>

                    <div className="mt-4 mb-1 px-[21px] flex items-center gap-2 text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                        <span className="flex-1">Clients</span>
                        {entries.length > 0 && <span className="tabular-nums">{entries.length}</span>}
                    </div>
                    <div className="space-y-px">
                        {entries.map((c) => (
                            <ClientRow
                                key={c.id}
                                client={c}
                                current={c.id === currentOrgId}
                                busy={entering === c.id}
                                disabled={pending}
                                onOpen={() => void openClient(c.id)}
                            />
                        ))}
                        {entries.length === 0 && !clients.isLoading && (
                            <p className="mx-3 px-[9px] py-1 text-[11.5px] text-slate-400">No clients yet.</p>
                        )}
                        {canManage && (
                            <button type="button" onClick={() => setAdding(true)} className={cn(ROW, "text-slate-500 hover:text-slate-900 hover:bg-slate-200/40")}>
                                <PlusIcon className="w-[14px] h-[14px] shrink-0 text-slate-400" strokeWidth={1.6} />
                                <span className="flex-1 min-w-0 truncate text-left">Add client</span>
                            </button>
                        )}
                    </div>
                </nav>

                <div className="border-t border-slate-200/60 py-1 shrink-0">
                    <Link to="/select-org" className={cn(ROW, "text-slate-600 hover:text-slate-900 hover:bg-slate-200/40")}>
                        <LayoutGridIcon className="w-[14px] h-[14px] shrink-0 text-slate-400" strokeWidth={1.6} />
                        <span className="flex-1 min-w-0 truncate">All workspaces</span>
                    </Link>
                </div>
            </aside>
            <AddClientDialog open={adding} onClose={() => setAdding(false)} agencyId={agency.id} clients={clients.data ?? []} />
        </>
    );
}

const HEALTH_DOT: Record<Health, { cls: string; label: string }> = {
    critical: { cls: "bg-red-500", label: "Needs attention" },
    warning: { cls: "bg-amber-500", label: "Has warnings" },
    ok: { cls: "bg-emerald-500", label: "Healthy" },
};

function ClientRow({
    client: c,
    current,
    busy,
    disabled,
    onOpen,
}: {
    client: ClientEntry;
    current: boolean;
    busy: boolean;
    disabled: boolean;
    onOpen: () => void;
}) {
    const dot = c.health ? HEALTH_DOT[c.health] : null;
    const body = (
        <>
            <ClientAvatar name={c.name} avatar={c.avatar} size="sm" />
            <span className="flex-1 min-w-0 truncate text-left">{c.name}</span>
            {busy ? (
                <Loader2Icon className="w-3 h-3 animate-spin text-slate-400 shrink-0" />
            ) : (
                dot && <span aria-label={dot.label} title={dot.label} className={cn("size-1.5 rounded-full shrink-0", dot.cls)} />
            )}
        </>
    );
    if (c.access === "none") {
        return (
            <span title="You are not a member of this workspace" className={cn(ROW, "text-slate-400 cursor-default")}>
                {body}
            </span>
        );
    }
    return (
        <button
            type="button"
            onClick={onOpen}
            disabled={disabled}
            title={`Open ${c.name}`}
            className={cn(ROW, current ? "text-slate-900" : "text-slate-600", "hover:text-slate-900 hover:bg-slate-200/40 disabled:opacity-60")}
        >
            {body}
        </button>
    );
}
