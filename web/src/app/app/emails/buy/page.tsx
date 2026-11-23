// Buy mailboxes: one screen. Search, the senders and the order fill the width; the summary and Pay sit in a sticky column; after Pay the
// summary grows into the live setup. One page for /app/emails/buy and /app/emails/buy/orders/$id, so paying
// morphs into the setup instead of remounting, and a refresh on the order lands on the same view.
import React from "react";
import { AnimatePresence, LayoutGroup, motion, useReducedMotion } from "framer-motion";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import toast from "react-hot-toast/headless";
import { ArrowLeftIcon, Loader2Icon, PauseCircleIcon, StoreIcon } from "lucide-react";
import { NoAccess } from "@/components/layout/NoAccess";
import { Page } from "@/components/layout/Page";
import DomainSearch from "@/components/app/store/DomainSearch";
import OrderList from "@/components/app/store/OrderList";
import Senders from "@/components/app/store/Senders";
import { SummaryPanel, SummarySheet } from "@/components/app/store/OrderSummary";
import SetupTrack from "@/components/app/store/SetupTrack";
import { approvalOutcome } from "@/lib/buy/approval";
import StoreLocked from "@/components/app/store/StoreLocked";
import { OrdersLink } from "@/components/app/store/StoreLinks";
import { useCheckout } from "@/components/app/store/useCheckout";
import { useUserProfile } from "@/hooks/context/user";
import { LG_QUERY, useMediaQuery } from "@/hooks/useMediaQuery";
import { usePermission } from "@/hooks/usePermission";
import { orderQuery, storeSalesPaused, storeState, useOrder, useStoreStatus } from "@/lib/api/hooks/app/store/useStore";
import usePartnerManaged from "@/hooks/usePartnerManaged";
import { storePhotoFromProfile } from "@/lib/api/client/app/store/store";
import { storeErrorCode } from "@/lib/buy/errors";
import type { DomainSearchResult, Order, StoreDomain, StoreProvider, StoreStatus } from "@/lib/api/models/app/store/Store";
import { cartReducer, DEFAULT_LIMITS, emptyCart, loadCart, saveCart, type CartLimits, type PriceBook } from "@/lib/buy/cart";
import { ownedFromDomain } from "@/lib/buy/connection";
import { EASE } from "@/lib/buy/motion";
import { useAppStore } from "@/stores/useAppStore";

export default function BuyMailboxesPage() {
    const canView = usePermission("MANAGE_EMAILS");
    const status = useStoreStatus(canView);
    const managed = usePartnerManaged() !== null;
    const orderId = useParams({ strict: false }).id;
    if (!canView) return <NoAccess feature="buying mailboxes" permissionLabel="Manage mailboxes" />;
    if (status.isPending) return <Shell />;
    const store = storeState(status.data, managed);
    // Paused sales still follow an order already placed.
    if (store.kind === "on" && storeSalesPaused(store.status) && !orderId) return <SalesPaused />;
    if (store.kind === "on") return <Store status={store.status} orderId={orderId} />;
    if (store.kind === "hidden") return <Unavailable failed={status.isError} />;
    // A workspace with live purchases can still follow an order it placed before GraphKit was disconnected.
    if (orderId && store.status.disconnected) return <Store status={store.status} orderId={orderId} />;
    return (
        <Shell orders={!!store.status.disconnected}>
            <StoreLocked status={store.status} />
        </Shell>
    );
}

function Store({ status, orderId }: { status: StoreStatus; orderId: string | undefined }) {
    const reduced = useReducedMotion();
    return (
        <Shell onOrder={!!orderId}>
            <LayoutGroup>
                <AnimatePresence mode="popLayout" initial={false}>
                    {orderId ? (
                        <motion.div
                            key="setup"
                            initial={reduced ? { opacity: 0 } : false}
                            animate={{ opacity: 1 }}
                            exit={{ opacity: 0 }}
                            transition={{ duration: 0.2, ease: EASE }}
                        >
                            <Setup id={orderId} />
                        </motion.div>
                    ) : (
                        <motion.div key="checkout" initial={false} exit={{ opacity: 0, transition: { duration: 0.18 } }}>
                            <Checkout status={status} />
                        </motion.div>
                    )}
                </AnimatePresence>
            </LayoutGroup>
        </Shell>
    );
}

function Checkout({ status }: { status: StoreStatus }) {
    const navigate = useNavigate();
    const wide = useMediaQuery(LG_QUERY);
    const orgId = useAppStore((s) => s.currentOrganization?.id ?? "");
    const { user } = useUserProfile();
    const person = React.useMemo(() => ({ first: user?.first_name ?? "", last: user?.last_name ?? "" }), [user?.first_name, user?.last_name]);
    const limits: CartLimits = {
        max: status.limits?.mailboxes_per_domain_max || DEFAULT_LIMITS.max,
        recommended: status.limits?.mailboxes_per_domain_recommended || DEFAULT_LIMITS.recommended,
    };
    const providers = status.providers?.length ? status.providers : (["google", "microsoft"] as const).slice();

    const [cart, dispatch] = React.useReducer(cartReducer, undefined, () => loadCart(orgId) ?? emptyCart(person));
    React.useEffect(() => saveCart(orgId, cart), [orgId, cart]);
    // A fresh cart's first sender picks up the account's name once the profile has loaded.
    React.useEffect(() => dispatch({ type: "seed_sender", person }), [person]);
    // A saved cart may name a provider that is no longer offered.
    const offered = providers.join(",");
    React.useEffect(() => dispatch({ type: "offered_providers", providers: offered.split(",") as StoreProvider[] }), [offered]);

    // New domains forward to the workspace's website (or the buyer's business domain) until the buyer changes it.
    const suggestedForward = status.default_forwarding_url ?? "";
    React.useEffect(() => {
        if (suggestedForward) dispatch({ type: "default_forwarding", value: suggestedForward });
    }, [suggestedForward]);

    // The first sender's photo starts as the buyer's own, copied for the order once a domain is in it.
    const profileUrl = status.photos ? (user?.avatar_url ?? null) : null;
    const profileAsked = React.useRef(false);
    const wantsProfile = !!profileUrl && cart.senders[0].photo === undefined && cart.domains.length > 0;
    React.useEffect(() => {
        if (!wantsProfile || profileAsked.current) return;
        profileAsked.current = true;
        storePhotoFromProfile()
            .then((p) => dispatch({ type: "seed_photo", photo: { id: p.id, at: Date.now() } }))
            .catch((e: unknown) => {
                // Only "you have none" is remembered; anything else is asked again on the next visit.
                if (storeErrorCode(e) === "profile_photo_missing") dispatch({ type: "seed_photo", photo: null });
            });
    }, [wantsProfile]);

    const [query, setQuery] = React.useState("");
    // Prices are only ever live: from the search a domain was added from, then from each quote.
    const [prices, setPrices] = React.useState<PriceBook>({});
    const mergePrices = React.useCallback((book: PriceBook) => setPrices((prev) => ({ ...prev, ...book })), []);

    const onPlaced = (o: Order) => {
        dispatch({ type: "reset" });
        // Replace, so Back leaves the store rather than returning to a checkout that already paid.
        void navigate({ to: "/app/emails/buy/orders/$id", params: { id: o.id }, replace: true });
    };
    const checkout = useCheckout({ cart, status, prices, onPrices: mergePrices, onPlaced });

    const inOrder = React.useCallback((d: string) => cart.domains.some((x) => x.domain === d), [cart.domains]);
    const onToggle = (r: DomainSearchResult) => {
        if (inOrder(r.domain)) dispatch({ type: "remove", domain: r.domain });
        else {
            mergePrices({ [r.domain]: { price_micros: r.price_micros, renew_micros: r.renew_micros } });
            dispatch({ type: "add", domain: r.domain, years: r.years });
        }
    };
    const onToggleOwned = (d: StoreDomain) => {
        if (inOrder(d.name)) dispatch({ type: "remove", domain: d.name });
        else dispatch({ type: "add_owned", owned: ownedFromDomain(d) });
    };

    const summary = { cart, status, prices, checkout };
    return (
        <div className="lg:grid lg:grid-cols-[minmax(0,1fr)_380px] 2xl:grid-cols-[minmax(0,1fr)_420px] lg:gap-8 2xl:gap-10 lg:items-start">
            <div className="min-w-0 pb-36 lg:pb-0 space-y-6">
                <div>
                    <h1 className="text-[22px] font-semibold tracking-[-0.02em] text-slate-900">Buy mailboxes</h1>
                    <p className="mt-0.5 text-[12.5px] text-slate-500 leading-relaxed">
                        A sending domain next to your main one, with Google or Microsoft mailboxes set up, connected and warming for you.
                    </p>
                </div>
                <DomainSearch
                    query={query}
                    onQuery={setQuery}
                    inOrder={inOrder}
                    onToggle={onToggle}
                    onToggleOwned={onToggleOwned}
                    atCost={status.domain_markup === "at_cost"}
                    hasOrder={cart.domains.length > 0}
                />
                <Senders cart={cart} dispatch={dispatch} limits={limits} providers={providers} photos={!!status.photos}
                    profileUrl={profileUrl}
                    nudged={checkout.nudge > 0}
                />
                <OrderList cart={cart} dispatch={dispatch} limits={limits} providers={providers} />
            </div>
            {wide ? (
                <aside className="sticky top-[60px]">
                    <SummaryPanel {...summary} />
                </aside>
            ) : (
                <SummarySheet {...summary} />
            )}
        </div>
    );
}

function Setup({ id }: { id: string }) {
    const reduced = useReducedMotion();
    const navigate = useNavigate();
    const order = useOrder(id);
    const paused = storeSalesPaused(useStoreStatus().data);
    useApprovalReturn(id);
    return (
        <div className="max-w-[680px] mx-auto">
            <motion.section
                layoutId={reduced ? undefined : "buy-panel"}
                aria-label="Order setup"
                className="rounded-2xl border border-slate-200 bg-white shadow-[0_1px_2px_rgba(15,23,42,0.04),0_16px_40px_-24px_rgba(15,23,42,0.28)] px-5 py-6 sm:px-7 sm:py-7"
            >
                <motion.div initial={reduced ? false : { opacity: 0 }} animate={{ opacity: 1 }} transition={{ delay: reduced ? 0 : 0.15, duration: 0.25 }}>
                    <SetupTrack order={order.data} loading={order.isPending} onBuyMore={paused ? undefined : () => void navigate({ to: "/app/emails/buy" })} />
                    {order.isError && !order.data && (
                        <p className="mt-6 text-[12.5px] text-slate-500">
                            This order could not be loaded. It may belong to another workspace.{" "}
                            <Link to="/app/emails/buy" className="text-sky-700 hover:text-sky-900 font-medium">
                                Start a new one
                            </Link>
                        </p>
                    )}
                </motion.div>
            </motion.section>
        </div>
    );
}

/** Says how the Microsoft 365 approval went when the administrator lands back here, then drops it from the address. */
function useApprovalReturn(orderId: string) {
    const queryClient = useQueryClient();
    React.useEffect(() => {
        const params = new URLSearchParams(window.location.search);
        const outcome = approvalOutcome(params.get("status"), params.get("domain"));
        if (!outcome) return;
        window.history.replaceState(window.history.state, "", window.location.pathname);
        if (outcome.kind === "success") toast.success(outcome.text);
        else if (outcome.kind === "error") toast.error(outcome.text);
        else toast(outcome.text);
        void queryClient.invalidateQueries({ queryKey: orderQuery(orderId).queryKey });
        void queryClient.invalidateQueries({ queryKey: ["store", "mailboxes"] });
    }, [orderId, queryClient]);
}

function Shell({ onOrder = false, orders = true, children }: { onOrder?: boolean; orders?: boolean; children?: React.ReactNode }) {
    return (
        <Page>
            <div className="sticky top-0 z-20 bg-white/90 backdrop-blur border-b border-slate-200">
                <div className="h-12 px-4 md:px-5 flex items-center gap-3">
                    <Link
                        to={onOrder ? "/app/emails/buy/orders" : "/app/emails"}
                        className="inline-flex items-center gap-1 h-6 -ml-1.5 px-1.5 rounded-md text-[11.5px] text-slate-500 hover:text-slate-900 hover:bg-slate-100 transition-colors shrink-0"
                    >
                        <ArrowLeftIcon className="w-3 h-3" />
                        {onOrder ? "Orders" : "Mailboxes"}
                    </Link>
                    <div className="h-4 w-px bg-slate-200 shrink-0" />
                    <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium truncate">{onOrder ? "Order" : "Buy mailboxes"}</span>
                    {!onOrder && orders && (
                        <div className="ml-auto flex items-center gap-2">
                            <OrdersLink />
                        </div>
                    )}
                </div>
            </div>
            <div className="flex-1 w-full min-w-0 px-4 md:px-6 xl:px-8 pt-5 pb-10">{children ?? <ShellSkeleton />}</div>
        </Page>
    );
}

function ShellSkeleton() {
    return (
        <div aria-busy="true" className="lg:grid lg:grid-cols-[minmax(0,1fr)_380px] 2xl:grid-cols-[minmax(0,1fr)_420px] lg:gap-8">
            <div className="space-y-3">
                <div className="h-6 w-48 rounded skeleton-shimmer" />
                <div className="h-3.5 w-80 max-w-full rounded skeleton-shimmer" />
                <div className="mt-4 h-12 rounded-xl skeleton-shimmer" />
                <div className="flex items-center justify-center py-10 text-slate-300">
                    <Loader2Icon className="w-4 h-4 animate-spin" />
                </div>
            </div>
            <div className="hidden lg:block h-64 rounded-2xl skeleton-shimmer" />
        </div>
    );
}

function SalesPaused() {
    return (
        <Shell>
            <div role="status" className="py-16 flex flex-col items-center text-center">
                <div className="size-10 rounded-full bg-amber-50 text-amber-600 inline-flex items-center justify-center mb-3">
                    <PauseCircleIcon className="w-4 h-4" />
                </div>
                <p className="text-[13px] text-slate-800 font-medium">Buying new mailboxes is paused</p>
                <p className="mt-1 text-[12px] text-slate-500 max-w-[46ch] leading-relaxed">
                    It will be available again soon. The domains and mailboxes you already bought keep working and renewing as usual, and you can still connect
                    mailboxes you already have.
                </p>
                <Link to="/app/emails" className="mt-4 h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] font-medium text-slate-700 inline-flex items-center">
                    Back to mailboxes
                </Link>
            </div>
        </Shell>
    );
}

function Unavailable({ failed }: { failed: boolean }) {
    return (
        <Shell>
            <div className="py-16 flex flex-col items-center text-center">
                <div className="size-10 rounded-full bg-slate-50 text-slate-400 inline-flex items-center justify-center mb-3">
                    <StoreIcon className="w-4 h-4" />
                </div>
                <p className="text-[13px] text-slate-800 font-medium">{failed ? "The store could not be reached" : "Buying mailboxes is not available here"}</p>
                <p className="mt-1 text-[12px] text-slate-500 max-w-[44ch] leading-relaxed">
                    {failed
                        ? "Try again in a moment. Your order is kept in this browser."
                        : "This instance does not sell domains or mailboxes. You can still connect mailboxes you already have."}
                </p>
                <Link to="/app/emails" className="mt-4 h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] font-medium text-slate-700 inline-flex items-center">
                    Back to mailboxes
                </Link>
            </div>
        </Shell>
    );
}
