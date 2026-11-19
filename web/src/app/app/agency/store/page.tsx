// Domains and mailboxes bought in the store across the agency's clients: what each costs a month, what renews
// soon and what needs a look. Every row opens that client's own Purchased views.
import type { useNavigate } from "@tanstack/react-router";
import { ChevronRightIcon, GlobeIcon, Loader2Icon, MailIcon, MoreHorizontalIcon, ShoppingBagIcon } from "lucide-react";
import { AgencyPage } from "@/components/app/agency/AgencyPage";
import { AgencySkeletonRows, SkeletonBar } from "@/components/app/agency/AgencySkeleton";
import { ClientAvatar } from "@/components/app/agency/home/AgencyHome";
import { formatNumber } from "@/components/app/agency/styles";
import { EmptyBlock, PageBody, SectionBar, Stat, StatStrip, TopbarAction } from "@/components/layout/Page";
import { PopoverMenu, PopoverMenuContent, PopoverMenuItem, PopoverMenuTrigger } from "@/components/ui/popover-menu";
import { useCurrentAgency } from "@/lib/api/hooks/app/agency/useAgency";
import { useAgencyLive, useOpenClient } from "@/lib/api/hooks/app/agency/useAgencyHome";
import { useAgencyStore } from "@/lib/api/hooks/app/agency/useAgencyStore";
import type { AgencyStoreAttention, AgencyStoreClient, AgencyStoreRenewal } from "@/lib/api/models/app/agency/AgencyStore";
import { fmtMicros, fmtMonthly, fmtYearly } from "@/lib/buy/money";
import { cn } from "@/lib/utils";

type Nav = ReturnType<typeof useNavigate>;

// Where each Purchased view lives inside a client workspace.
const toDomains = (nav: Nav) => nav({ to: "/app/emails/domains", search: { tab: "purchased" } });
const toMailboxes = (nav: Nav) => nav({ to: "/app/emails" });
const toOrders = (nav: Nav) => nav({ to: "/app/emails/buy/orders" });

const ATTENTION: Record<string, { one: string; many: string; tone: "red" | "amber" }> = {
    connect_failed: { one: "mailbox not connected", many: "mailboxes not connected", tone: "red" },
    billing_failed: { one: "mailbox payment failed", many: "mailbox payments failed", tone: "red" },
    renewal_failed: { one: "renewal failed", many: "renewals failed", tone: "red" },
    order_failed: { one: "order failed", many: "orders failed", tone: "amber" },
    auto_renew_off: { one: "auto-renew off", many: "auto-renew off", tone: "amber" },
};

function attentionScore(c: AgencyStoreClient): number {
    return c.attention.reduce((n, a) => n + a.count * (ATTENTION[a.kind]?.tone === "red" ? 100 : 1), 0);
}

function asDate(v: Date | string | null | undefined): Date | null {
    if (!v) return null;
    const d = v instanceof Date ? v : new Date(v);
    return Number.isNaN(d.getTime()) ? null : d;
}

const fmtDay = (v: Date | string | null | undefined) => asDate(v)?.toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" }) ?? "";

function daysUntil(v: Date | string): number | null {
    const d = asDate(v);
    return d ? Math.ceil((d.getTime() - Date.now()) / 86_400_000) : null;
}

function AgencyStore() {
    const { agency } = useCurrentAgency();
    const store = useAgencyStore(agency?.id);
    const { open, opening, pending } = useOpenClient();
    useAgencyLive(agency?.id);
    if (!agency) return null;

    const data = store.data;
    if (store.isPending) {
        return (
            <PageBody>
                <StatStrip cols={5}>
                    {["Domains", "Live mailboxes", "Mailboxes a month", "Renewals in 30 days", "Needs attention"].map((l, i) => (
                        <Stat key={l} label={l} value={<SkeletonBar className="h-5 w-12 mt-0.5" />} last={i === 4} />
                    ))}
                </StatStrip>
                <AgencySkeletonRows rows={4} />
            </PageBody>
        );
    }
    if (!data) return <EmptyBlock title="Purchased domains and mailboxes could not be loaded" body="Try again in a moment." />;
    if (!data.enabled) {
        return <EmptyBlock title="Not available on this instance" body="This instance does not sell domains or mailboxes. Your clients can still connect the mailboxes they have." />;
    }
    if (data.clients.length === 0) {
        return (
            <EmptyBlock
                title="No purchased domains or mailboxes yet"
                body="Open a client and buy mailboxes from its Mailboxes page. They show up here with their renewals and what they cost each month."
                cta={<TopbarAction href="/app/agency/clients">Open a client</TopbarAction>}
            />
        );
    }

    const t = data.totals;
    const clients = [...data.clients].sort((a, b) => attentionScore(b) - attentionScore(a) || a.name.localeCompare(b.name));
    const go = (orgId: string, then: (nav: Nav) => unknown) => void open(orgId, then);

    return (
        <PageBody>
            <StatStrip cols={5}>
                <Stat animated label="Domains" value={t.domains} />
                <Stat animated label="Live mailboxes" value={t.mailboxes_live} />
                <Stat label="Mailboxes a month" value={fmtMicros(t.monthly_spend_micros)} sub="from each client's balance" />
                <Stat animated label="Renewals in 30 days" value={t.renewals_30d} />
                <Stat animated label="Needs attention" value={t.attention} tone="warn" last />
            </StatStrip>

            <SectionBar label="Clients" count={clients.length} />
            <div className="hidden lg:flex h-8 px-5 items-center gap-3 border-b border-slate-200/60 text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                <span className="flex-1 min-w-0">Client</span>
                <span className="w-16 text-right">Domains</span>
                <span className="w-28 text-right">Mailboxes</span>
                <span className="w-24 text-right">Monthly</span>
                <span className="w-36">Next renewal</span>
                <span className="w-56">Attention</span>
                <span className="w-7" />
            </div>
            {clients.map((c) => (
                <ClientRow
                    key={c.organization_id}
                    client={c}
                    opening={opening === c.organization_id}
                    disabled={pending}
                    onOpen={(then) => go(c.organization_id, then)}
                />
            ))}

            <SectionBar label="Renewals in the next 30 days" count={data.renewals.length} />
            {data.renewals.length === 0 ? (
                <p className="px-5 py-3 text-[12px] text-slate-500 border-b border-slate-200/60">Nothing renews in the next 30 days.</p>
            ) : (
                data.renewals.map((r) => (
                    <RenewalRow
                        key={r.domain_id}
                        renewal={r}
                        opening={opening === r.organization_id}
                        disabled={pending}
                        onOpen={() => go(r.organization_id, toDomains)}
                    />
                ))
            )}
            <p className="px-5 py-4 text-[11.5px] text-slate-500 leading-relaxed max-w-[72ch]">
                Each client pays for its own domains and mailboxes from its workspace balance. Only clients where you manage mailboxes are listed.
            </p>
        </PageBody>
    );
}

function Chip({ a }: { a: AgencyStoreAttention }) {
    if (a.count <= 0) return null;
    const c = ATTENTION[a.kind] ?? { one: a.kind.replace(/_/g, " "), many: a.kind.replace(/_/g, " "), tone: "amber" as const };
    return (
        <span
            className={cn(
                "h-5 px-1.5 rounded text-[10.5px] font-medium inline-flex items-center whitespace-nowrap",
                c.tone === "red" ? "bg-red-50 text-red-700" : "bg-amber-50 text-amber-700",
            )}
        >
            {formatNumber(a.count)} {a.count === 1 ? c.one : c.many}
        </span>
    );
}

function ClientRow({
    client: c,
    opening,
    disabled,
    onOpen,
}: {
    client: AgencyStoreClient;
    opening: boolean;
    disabled: boolean;
    onOpen: (then: (nav: Nav) => unknown) => void;
}) {
    const enter = disabled ? undefined : () => onOpen(toDomains);
    const unconnected = c.mailboxes_live - c.mailboxes_connected;
    const next = c.next_renewal;
    const nextDays = next ? daysUntil(next.expires_at) : null;
    const chips = (
        <>
            {c.orders_open > 0 && (
                <button
                    type="button"
                    onClick={(e) => {
                        e.stopPropagation();
                        if (!disabled) onOpen(toOrders);
                    }}
                    className="h-5 px-1.5 rounded bg-sky-50 text-sky-700 text-[10.5px] font-medium inline-flex items-center gap-1 whitespace-nowrap hover:bg-sky-100"
                >
                    <span className="size-1.5 rounded-full bg-sky-500 animate-pulse motion-reduce:animate-none" />
                    {formatNumber(c.orders_open)} setting up
                </button>
            )}
            {c.attention.map((a) => (
                <Chip key={a.kind} a={a} />
            ))}
        </>
    );
    const hasChips = c.orders_open > 0 || c.attention.some((a) => a.count > 0);

    return (
        <div
            role="button"
            tabIndex={0}
            aria-busy={opening || undefined}
            onClick={enter}
            onKeyDown={(e) => {
                if (enter && e.key === "Enter" && e.target === e.currentTarget) enter();
            }}
            className={cn(
                "group min-h-11 px-5 py-2 lg:py-1.5 flex flex-wrap lg:flex-nowrap items-center gap-x-3 gap-y-1.5 border-b border-slate-200/60 transition-colors hover:bg-slate-50/80 cursor-pointer",
                opening && "bg-slate-50/80",
            )}
        >
            <span className="flex-1 min-w-0 flex items-center gap-2">
                <ClientAvatar name={c.name} avatar={c.avatar_url} />
                <span className="text-[12.5px] text-slate-900 font-medium truncate">{c.name}</span>
            </span>
            <span className="w-16 text-right text-[12px] tabular-nums text-slate-700 hidden lg:inline">{formatNumber(c.domains)}</span>
            <span className="w-28 text-right text-[12px] tabular-nums text-slate-700 hidden lg:inline">
                {formatNumber(c.mailboxes_live)}
                {unconnected > 0 && <span className="block text-[11px] text-amber-700">{formatNumber(c.mailboxes_connected)} connected</span>}
            </span>
            <span className="w-24 text-right text-[12px] tabular-nums text-slate-700 hidden lg:inline">{c.monthly_spend_micros > 0 ? fmtMonthly(c.monthly_spend_micros) : "-"}</span>
            <span className="w-36 text-[12px] hidden lg:block min-w-0">
                {next ? (
                    <>
                        <span className="block truncate text-slate-700">{next.domain}</span>
                        <span className={cn("block text-[11px] tabular-nums", nextDays !== null && nextDays <= 7 && !next.auto_renew ? "text-red-600" : "text-slate-500")}>
                            {next.auto_renew ? "Renews" : "Expires"} {fmtDay(next.expires_at)}
                        </span>
                    </>
                ) : (
                    <span className="text-slate-300">-</span>
                )}
            </span>
            <span className="w-56 hidden lg:flex items-center gap-1 flex-wrap">{hasChips ? chips : <span className="text-[12px] text-slate-400">None</span>}</span>
            {/* Narrow screens: the same facts as labelled pairs. */}
            <div className="lg:hidden w-full flex flex-wrap items-center gap-x-4 gap-y-1 text-[11.5px] text-slate-500">
                <span>
                    Domains <span className="tabular-nums text-slate-700">{formatNumber(c.domains)}</span>
                </span>
                <span>
                    Mailboxes <span className="tabular-nums text-slate-700">{formatNumber(c.mailboxes_live)}</span>
                    {unconnected > 0 && <span className="text-amber-700"> ({formatNumber(c.mailboxes_connected)} connected)</span>}
                </span>
                {c.monthly_spend_micros > 0 && <span className="tabular-nums text-slate-700">{fmtMonthly(c.monthly_spend_micros)}</span>}
                {next && (
                    <span>
                        {next.auto_renew ? "Renews" : "Expires"} {fmtDay(next.expires_at)}
                    </span>
                )}
                {hasChips && <span className="flex items-center gap-1 flex-wrap">{chips}</span>}
            </div>
            {opening ? (
                <span className="size-7 inline-flex items-center justify-center" aria-label={`Opening ${c.name}`}>
                    <Loader2Icon className="w-3.5 h-3.5 animate-spin text-slate-400" />
                </span>
            ) : (
                <div onClick={(e) => e.stopPropagation()} onKeyDown={(e) => e.stopPropagation()}>
                    <PopoverMenu align="end">
                        <PopoverMenuTrigger asChild>
                            <button
                                type="button"
                                disabled={disabled}
                                aria-label={`Open ${c.name}'s purchases`}
                                className="size-7 rounded-md text-slate-400 hover:text-slate-800 hover:bg-slate-100 inline-flex items-center justify-center opacity-100 md:opacity-0 md:group-hover:opacity-100 focus:opacity-100 disabled:opacity-40"
                            >
                                <MoreHorizontalIcon className="w-3.5 h-3.5" />
                            </button>
                        </PopoverMenuTrigger>
                        <PopoverMenuContent minWidth={200}>
                            <PopoverMenuItem icon={<GlobeIcon className="w-3 h-3" />} onSelect={() => onOpen(toDomains)} description="Renewals, forwarding and DNS">
                                Purchased domains
                            </PopoverMenuItem>
                            <PopoverMenuItem icon={<MailIcon className="w-3 h-3" />} onSelect={() => onOpen(toMailboxes)}>
                                Mailboxes
                            </PopoverMenuItem>
                            <PopoverMenuItem icon={<ShoppingBagIcon className="w-3 h-3" />} onSelect={() => onOpen(toOrders)}>
                                Orders
                            </PopoverMenuItem>
                        </PopoverMenuContent>
                    </PopoverMenu>
                </div>
            )}
        </div>
    );
}

function RenewalRow({ renewal: r, opening, disabled, onOpen }: { renewal: AgencyStoreRenewal; opening: boolean; disabled: boolean; onOpen: () => void }) {
    const days = daysUntil(r.expires_at);
    const urgent = !r.auto_renew && days !== null && days <= 7;
    return (
        <button
            type="button"
            disabled={disabled}
            aria-busy={opening || undefined}
            onClick={onOpen}
            className={cn(
                "group w-full min-h-10 px-5 py-1.5 flex flex-wrap sm:flex-nowrap items-center gap-x-3 gap-y-0.5 border-b border-slate-200/60 text-left hover:bg-slate-50/80 transition-colors",
                opening ? "bg-slate-50/80" : "disabled:opacity-60",
            )}
        >
            <span className={cn("w-28 shrink-0 text-[12px] tabular-nums", urgent ? "text-red-600 font-medium" : "text-slate-600")}>{fmtDay(r.expires_at)}</span>
            <span className="flex-1 min-w-0 truncate text-[12.5px] text-slate-900 font-medium">{r.domain}</span>
            <span className="w-40 min-w-0 truncate text-[12px] text-slate-500">{r.organization_name}</span>
            <span className="w-24 text-right text-[12px] tabular-nums text-slate-700">{fmtYearly(r.renew_micros)}</span>
            <span className="w-24 shrink-0">
                {r.auto_renew ? (
                    <span className="h-5 px-1.5 rounded bg-slate-100 text-slate-600 text-[10.5px] font-medium inline-flex items-center">Auto-renew</span>
                ) : (
                    <span className="h-5 px-1.5 rounded bg-amber-50 text-amber-700 text-[10.5px] font-medium inline-flex items-center">Auto-renew off</span>
                )}
            </span>
            {opening ? (
                <Loader2Icon className="w-3.5 h-3.5 animate-spin text-slate-400 shrink-0" />
            ) : (
                <ChevronRightIcon className="w-3.5 h-3.5 text-slate-300 group-hover:text-slate-600 group-hover:translate-x-0.5 transition-transform shrink-0" />
            )}
        </button>
    );
}

export default function AgencyStorePage() {
    return (
        <AgencyPage title="Domains & mailboxes">
            <AgencyStore />
        </AgencyPage>
    );
}
