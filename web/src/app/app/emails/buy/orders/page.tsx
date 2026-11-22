// Orders: every store order this workspace placed, newest first. An open one carries its setup track live; a row opens the order.
import type { ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import { motion, useReducedMotion } from "framer-motion";
import { ArrowLeftIcon, ChevronRightIcon, Loader2Icon, ShoppingBagIcon } from "lucide-react";
import { NoAccess } from "@/components/layout/NoAccess";
import { Page } from "@/components/layout/Page";
import ProviderLogo from "@/components/app/emails/ProviderLogo";
import { BuyMailboxesButton } from "@/components/app/store/BuyEntry";
import { CompactTrack } from "@/components/app/store/SetupTrack";
import { StoreChargesLink } from "@/components/app/store/StoreLinks";
import { usePermission } from "@/hooks/usePermission";
import { isOpenOrder, useStoreOrders } from "@/lib/api/hooks/app/store/useStore";
import type { Order, OrderStatus, StoreProvider } from "@/lib/api/models/app/store/Store";
import { storeErrorCode, storeErrorMessage } from "@/lib/buy/errors";
import { fmtMicros } from "@/lib/buy/money";
import { cn } from "@/lib/utils";

const STATUS: Record<OrderStatus, { label: string; tone: string }> = {
    charging: { label: "Confirming payment", tone: "bg-sky-50 text-sky-700 border-sky-100" },
    processing: { label: "Setting up", tone: "bg-sky-50 text-sky-700 border-sky-100" },
    completed: { label: "Completed", tone: "bg-emerald-50 text-emerald-700 border-emerald-100" },
    partially_failed: { label: "Partly refunded", tone: "bg-amber-50 text-amber-800 border-amber-200" },
    failed: { label: "Refunded", tone: "bg-red-50 text-red-700 border-red-100" },
};

const COLS = "md:grid-cols-[minmax(0,1.7fr)_128px_minmax(0,0.9fr)_88px_88px_88px_20px]";

export default function OrdersPage() {
    const canView = usePermission("MANAGE_EMAILS");
    if (!canView) return <NoAccess feature="store orders" permissionLabel="Manage mailboxes" />;
    return <Orders />;
}

function Orders() {
    const orders = useStoreOrders();
    const rows = orders.data?.pages.flatMap((p) => p.data) ?? [];
    const disabled = orders.isError && storeErrorCode(orders.error) === "store_disabled";

    return (
        <Page>
            <div className="sticky top-0 z-10 bg-white/90 backdrop-blur border-b border-slate-200">
                <div className="min-h-12 px-4 md:px-5 py-1.5 flex flex-wrap items-center gap-x-3 gap-y-1.5">
                    <Link
                        to="/app/emails/domains"
                        search={{ tab: "purchased" }}
                        className="inline-flex items-center gap-1 h-6 -ml-1.5 px-1.5 rounded-md text-[11.5px] text-slate-500 hover:text-slate-900 hover:bg-slate-100 transition-colors shrink-0"
                    >
                        <ArrowLeftIcon className="w-3 h-3" />
                        Purchased
                    </Link>
                    <div className="h-4 w-px bg-slate-200 shrink-0" />
                    <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Orders</span>
                    <div className="ml-auto flex items-center gap-1.5">
                        <StoreChargesLink />
                        {!disabled && <BuyMailboxesButton size="sm" />}
                    </div>
                </div>
            </div>

            {orders.isPending ? (
                <RowsSkeleton />
            ) : orders.isError && rows.length === 0 ? (
                <Empty
                    title={disabled ? "Buying mailboxes is not available here" : "Orders could not be loaded"}
                    body={disabled ? "This instance does not sell domains or mailboxes." : storeErrorMessage(orders.error)}
                />
            ) : rows.length === 0 ? (
                <Empty
                    title="No orders yet"
                    body="Domains and mailboxes you buy here show up as orders, with what each one cost and how its setup went."
                    cta={<BuyMailboxesButton size="sm" />}
                />
            ) : (
                <>
                    <div className={cn("hidden md:grid gap-x-3 items-center px-5 h-8 border-b border-slate-200/60 bg-slate-50/40", COLS)}>
                        {["Order", "Status", "Placed", "Total", "Charged", "Refunded", ""].map((h, i) => (
                            <span key={i} className={cn("text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em]", i >= 3 && i <= 5 && "text-right")}>
                                {h}
                            </span>
                        ))}
                    </div>
                    {rows.map((o, i) => (
                        <OrderRow key={o.id} order={o} index={i} />
                    ))}
                    {orders.hasNextPage && (
                        <div className="px-5 py-3">
                            <button
                                type="button"
                                onClick={() => void orders.fetchNextPage()}
                                disabled={orders.isFetchingNextPage}
                                className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 transition-colors inline-flex items-center gap-1.5 disabled:opacity-60"
                            >
                                {orders.isFetchingNextPage && <Loader2Icon className="w-3 h-3 animate-spin" />}
                                Load more
                            </button>
                        </div>
                    )}
                </>
            )}
        </Page>
    );
}

function asDate(v: Date | string | null | undefined): Date | null {
    if (!v) return null;
    const d = v instanceof Date ? v : new Date(v);
    return Number.isNaN(d.getTime()) ? null : d;
}

const fmtPlaced = (d: Date | null) =>
    d ? d.toLocaleString(undefined, { month: "short", day: "numeric", year: "numeric", hour: "numeric", minute: "2-digit" }) : "";

/** "getacme.com, tryacme.io +1 · 6 mailboxes". Empty until the order lists its items. */
function summary(o: Order): { domains: string; mailboxes: string; providers: StoreProvider[] } {
    const names = o.domains.map((d) => d.name);
    const domains = names.length <= 2 ? names.join(", ") : `${names.slice(0, 2).join(", ")} +${names.length - 2}`;
    const n = o.mailboxes.length;
    const mailboxes = n === 0 ? "" : `${n.toLocaleString()} ${n === 1 ? "mailbox" : "mailboxes"}`;
    const providers = [...new Set(o.mailboxes.map((m) => m.provider))];
    return { domains, mailboxes, providers };
}

function Money({ micros, muted, label }: { micros: number; muted?: boolean; label: string }) {
    return (
        <span className={cn("md:text-right text-[12px] tabular-nums", micros === 0 || muted ? "text-slate-400" : "text-slate-700")}>
            <span className="md:hidden text-slate-400">{label} </span>
            {micros === 0 && muted ? "-" : fmtMicros(micros)}
        </span>
    );
}

function OrderRow({ order: o, index }: { order: Order; index: number }) {
    const reduced = useReducedMotion();
    const s = STATUS[o.status] ?? { label: o.status, tone: "bg-slate-50 text-slate-600 border-slate-200" };
    const { domains, mailboxes, providers } = summary(o);
    const open = isOpenOrder(o);
    return (
        <motion.div initial={reduced ? false : { opacity: 0, y: 4 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: Math.min(index, 10) * 0.02, duration: 0.2 }}>
            <Link
                to="/app/emails/buy/orders/$id"
                params={{ id: o.id }}
                className="group block px-5 py-2.5 border-b border-slate-200/60 hover:bg-slate-50/80 transition-colors outline-none focus-visible:bg-slate-50"
            >
                <div className={cn("grid grid-cols-[minmax(0,1fr)_auto] gap-x-3 gap-y-1 items-center", COLS)}>
                    <div className="flex items-center gap-2.5 min-w-0">
                        <span className="flex -space-x-1 shrink-0">
                            {providers.length > 0 ? (
                                providers.map((p) => <ProviderLogo key={p} id={p} size="md" />)
                            ) : (
                                <span className="size-6 rounded-md bg-slate-50 border border-slate-200 text-slate-400 inline-flex items-center justify-center">
                                    <ShoppingBagIcon className="w-3 h-3" />
                                </span>
                            )}
                        </span>
                        <span className="min-w-0">
                            <span className="block text-[12.5px] text-slate-900 font-medium truncate">{domains || `Order ${o.id.slice(0, 8)}`}</span>
                            <span className="block text-[11.5px] text-slate-500 truncate">{mailboxes || (o.domains.length > 0 ? "Domain only" : "Items appear once the order is placed")}</span>
                        </span>
                    </div>
                    <span className="justify-self-end md:justify-self-start">
                        <span className={cn("h-[18px] px-1.5 rounded-full border text-[10.5px] font-medium inline-flex items-center gap-1 whitespace-nowrap", s.tone)}>
                            {open && <span className="size-1.5 rounded-full bg-sky-500 animate-pulse motion-reduce:animate-none" />}
                            {s.label}
                        </span>
                    </span>
                    <span className="text-[11.5px] md:text-[12px] text-slate-500 tabular-nums truncate max-md:col-span-2">{fmtPlaced(asDate(o.created_at))}</span>
                    <div className="max-md:col-span-2 flex md:contents gap-x-4 flex-wrap">
                        <Money micros={o.due_now_micros} label="Total" />
                        <Money micros={o.charged_micros} label="Charged" />
                        <Money micros={o.refunded_micros} label="Refunded" muted />
                    </div>
                    <ChevronRightIcon className="hidden md:block w-3.5 h-3.5 text-slate-300 group-hover:text-slate-600 group-hover:translate-x-0.5 transition-transform" />
                </div>
                {open && <CompactTrack order={o} className="mt-2 md:max-w-[420px] md:ml-[34px]" />}
            </Link>
        </motion.div>
    );
}

function RowsSkeleton() {
    return (
        <div aria-busy="true">
            {Array.from({ length: 4 }).map((_, i) => (
                <div key={i} className="px-5 py-3 border-b border-slate-200/60 flex items-center gap-3">
                    <div className="size-6 rounded-md skeleton-shimmer" />
                    <div className="h-3 w-40 rounded skeleton-shimmer" />
                    <div className="ml-auto h-4 w-16 rounded-full skeleton-shimmer" />
                </div>
            ))}
        </div>
    );
}

function Empty({ title, body, cta }: { title: string; body: string; cta?: ReactNode }) {
    return (
        <div className="px-5 py-16 flex flex-col items-center text-center">
            <div className="size-10 rounded-full bg-sky-50 text-sky-600 inline-flex items-center justify-center mb-3">
                <ShoppingBagIcon className="w-4 h-4" />
            </div>
            <p className="text-[13px] text-slate-800 font-medium">{title}</p>
            <p className="mt-1 text-[12px] text-slate-500 max-w-[48ch] leading-relaxed">{body}</p>
            {cta && <div className="mt-4">{cta}</div>}
        </div>
    );
}
