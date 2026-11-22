// Everything the order summary decides: the live quote, the registrant saved quietly behind it, the card
// saved in the page, and one Pay that does all of it in order. The total on screen is the one sent; a price
// that moved is shown and waits for another press, never paid on its own.
import React from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import toast from "react-hot-toast/headless";
import useDebouncedValue from "@/hooks/useDebouncedValue";
import { useUserProfile } from "@/hooks/context/user";
import { usePermission } from "@/hooks/usePermission";
import type { AppError } from "@/lib/api/client/normalizeError";
import { adoptCardSetup, topUpBalance } from "@/lib/api/client/app/subscription/balance";
import { balanceQuery, useAddPaymentMethod, useBalance } from "@/lib/api/hooks/app/subscription/useBalance";
import { quoteQuery, STORE_KEY, useCreateOrder, useQuote, useSaveRegistrant } from "@/lib/api/hooks/app/store/useStore";
import type { CreateOrderRequest, Order, Quote, Registrant, StoreCard, StoreStatus } from "@/lib/api/models/app/store/Store";
import { addressIssue, cartBlocker, cartEstimate, forwardIssue, orderForwarding, quoteItems, registersDomain, sendersIssue, type Cart, type PriceBook } from "@/lib/buy/cart";
import { payBlocker, payChecklist, payLabel, paymentPlan, withTopUp, type CardEntry, type PaymentPlan, type ReloadView } from "@/lib/buy/checkout";
import { isPlanRequired, refusedRegistrantFields, repricedQuote, storeErrorCode, storeErrorMessage } from "@/lib/buy/errors";
import { blankRegistrant, registrantComplete, registrantGaps, type RegistrantField } from "@/lib/buy/registrant";
import { resumeKey, saveResume, settleResume, takeResume } from "@/lib/buy/resume";
import { minFundUsd, nextPresetAbove } from "@/components/app/billing/funds";
import type { CardFieldHandle, CardFieldState } from "./CardField";

// Answers that end a request without running it free its idempotency key; anything else is final for that key.
const KEY_FREED = new Set([401, 403, 429]);
const DOMAIN_REFUSED = new Set(["domain_unavailable", "domain_premium", "tld_unsupported"]);
// Typing an address re-prices the order once the typing stops.
const QUOTE_DEBOUNCE_MS = 450;
// A registrant finished by hand is saved once the typing stops.
const REGISTRANT_DEBOUNCE_MS = 800;

export type Busy = null | "pricing" | "card" | "paying" | "redirecting";

const nonBlank = (o: object | null | undefined) => Object.fromEntries(Object.entries(o ?? {}).filter(([, v]) => typeof v === "string" && v.trim()));

export function useCheckout({
    cart,
    status,
    prices,
    onPrices,
    onPlaced,
}: {
    cart: Cart;
    status: StoreStatus;
    prices: PriceBook;
    onPrices: (book: PriceBook) => void;
    onPlaced: (order: Order) => void;
}) {
    const qc = useQueryClient();
    const navigate = useNavigate();
    const { user } = useUserProfile();
    const canPay = usePermission("MANAGE_BILLING");
    const saveRegistrant = useSaveRegistrant();
    const createOrder = useCreateOrder();
    const addCard = useAddPaymentMethod();
    const metered = status.metered;

    const items = React.useMemo(() => quoteItems(cart), [cart]);
    const itemsKey = JSON.stringify(items);
    const cartIssue = cartBlocker(cart);
    const registers = registersDomain(cart);

    const [busy, setBusy] = React.useState<Busy>(null);
    const [error, setError] = React.useState<string | null>(null);
    const [priceNotice, setPriceNotice] = React.useState(false);
    const [orderNeedsPlan, setOrderNeedsPlan] = React.useState(false);
    const [nudge, setNudge] = React.useState(0);

    // ---- registrant: saved quietly from billing details and the account; asked for only where it has gaps ----
    const seed = React.useCallback(
        (): Registrant =>
            status.registrant ??
            blankRegistrant({ first_name: user?.first_name ?? "", last_name: user?.last_name ?? "", email: user?.email ?? "", ...nonBlank(status.registrant_prefill) }),
        [status.registrant, status.registrant_prefill, user?.first_name, user?.last_name, user?.email],
    );
    const [registrant, setRegistrant] = React.useState<Registrant>(seed);
    const [registrantTouched, setRegistrantTouched] = React.useState(false);
    // Until typed into here, the draft follows what the server holds (a teammate may fill it in).
    React.useEffect(() => {
        if (!registrantTouched) setRegistrant(seed());
    }, [seed, registrantTouched]);
    const [refused, setRefused] = React.useState<RegistrantField[]>([]);
    const [registrantError, setRegistrantError] = React.useState<string | null>(null);
    const [showRegistrantErrors, setShowRegistrantErrors] = React.useState(false);
    const savedComplete = registrantComplete(status.registrant);
    const registrantNeeded = registers && (!savedComplete || refused.length > 0);
    const gaps = registrantGaps(registrant, refused);
    // The fields asked for stay put while they are typed into, so a field never vanishes under the cursor.
    const [asked, setAsked] = React.useState<RegistrantField[]>([]);
    const gapsKey = gaps.join(",");
    React.useEffect(() => {
        if (!registrantNeeded) {
            setAsked([]);
            setShowRegistrantErrors(false);
            return;
        }
        const now = gapsKey ? (gapsKey.split(",") as RegistrantField[]) : [];
        setAsked((prev) => (now.every((f) => prev.includes(f)) ? prev : [...prev, ...now.filter((f) => !prev.includes(f))]));
    }, [registrantNeeded, gapsKey]);

    const refuseRegistrant = React.useCallback((e: unknown) => {
        setShowRegistrantErrors(true);
        setRefused(refusedRegistrantFields(e));
        setRegistrantError(storeErrorMessage(e));
    }, []);
    const editRegistrant = (field: RegistrantField, value: string) => {
        setRegistrantTouched(true);
        setRegistrantError(null);
        setRefused((prev) => prev.filter((f) => f !== field));
        setRegistrant((r) => ({ ...r, [field]: value }));
    };

    // Saved with no press: a complete prefill at once, a hand-finished one when the typing stops.
    const settledRegistrant = useDebouncedValue(registrant, registrantTouched ? REGISTRANT_DEBOUNCE_MS : 0);
    const savedFor = React.useRef("");
    const { mutate: saveRegistrantQuietly } = saveRegistrant;
    React.useEffect(() => {
        if (!registrantNeeded || refused.length > 0 || !canPay || !registrantComplete(settledRegistrant)) return;
        const key = JSON.stringify(settledRegistrant);
        if (savedFor.current === key) return;
        savedFor.current = key;
        saveRegistrantQuietly(settledRegistrant, { onSuccess: () => setRegistrantError(null), onError: refuseRegistrant });
    }, [settledRegistrant, registrantNeeded, refused.length, canPay, refuseRegistrant, saveRegistrantQuietly]);

    // ---- quote ----
    const quotedItems = useDebouncedValue(items, QUOTE_DEBOUNCE_MS);
    const planNeeded = status.plan_active === false || orderNeedsPlan;
    const quoteReady = cart.domains.length > 0 && !cartIssue && !registrantNeeded && !planNeeded;
    const quote = useQuote(quotedItems, quoteReady);
    const planFromQuote = isPlanRequired(quote.error);
    // A quote priced for an order the buyer has since changed is not shown.
    const q: Quote | undefined = quoteReady && JSON.stringify(quotedItems) === itemsKey ? quote.data : undefined;
    React.useEffect(() => {
        if (planNeeded || planFromQuote) void qc.invalidateQueries({ queryKey: [...STORE_KEY, "status"] });
    }, [planNeeded, planFromQuote, qc]);

    // The same order priced again at a different total means the price moved under the buyer.
    const seen = React.useRef<{ key: string; due: number } | null>(null);
    const itemsKeyRef = React.useRef(itemsKey);
    itemsKeyRef.current = itemsKey;
    React.useEffect(() => {
        if (!q) return;
        const key = itemsKeyRef.current;
        if (seen.current?.key === key && seen.current.due !== q.due_now_micros) setPriceNotice(true);
        seen.current = { key, due: q.due_now_micros };
        const book: PriceBook = {};
        for (const l of q.lines) if (l.type === "domain") book[l.domain] = { price_micros: l.amount_micros, renew_micros: l.renew_micros };
        onPrices(book);
    }, [q, onPrices]);

    const estimate = cartEstimate(cart, prices, status.mailbox_monthly_micros);
    const due = q?.due_now_micros ?? estimate?.due ?? null;
    const monthly = q?.recurring_monthly_micros ?? estimate?.monthly ?? 0;
    const yearly = q?.yearly_domain_renewal_micros ?? estimate?.yearly ?? null;

    // ---- payment: the balance first, then the card, and whether a card is asked for at all ----
    const savedCard: StoreCard | null = q?.payment.card ?? status.balance?.card ?? null;
    const balance = q?.payment.balance_micros ?? status.balance?.balance_micros ?? 0;
    const basePlan: PaymentPlan | null = metered ? paymentPlan(q?.payment, balance, due) : null;
    // A short balance can take a round preset instead of the bare shortfall; the rest stays on the balance.
    const topUpOffer = basePlan && !basePlan.balanceCovers && basePlan.cardCharge > 0 && canPay ? nextPresetAbove(basePlan.cardCharge / 1_000_000) : null;
    const [topUpTo, setTopUpTo] = React.useState<number | null>(null);
    const topUp = topUpTo !== null && topUpTo === topUpOffer ? topUpTo : null;
    const plan: PaymentPlan | null = basePlan && topUp ? withTopUp(basePlan, topUp * 1_000_000, due) : basePlan;
    const [cardState, setCardState] = React.useState<CardFieldState>({ status: "loading", complete: false });
    const cardRef = React.useRef<CardFieldHandle>(null);
    const publishableKey = status.stripe_publishable_key ?? "";
    const cardEntry: CardEntry = publishableKey && cardState.status !== "failed" ? "inline" : "redirect";
    const wantsCard = metered && canPay && !planNeeded && !savedCard && cart.domains.length > 0 && !plan?.balanceCovers;
    const showCardField = wantsCard && !!publishableKey;

    // The first purchase turns the automatic top-up on unless switched off here; one already on is shown, not offered.
    const reloadNow = useBalance(metered && canPay).data?.auto_reload?.enabled ?? false;
    const [autoReload, setAutoReload] = React.useState(true);
    const ar = plan?.autoReload ?? null;
    const minCharge = Math.max(status.min_card_charge_micros ?? 0, minFundUsd(status.min_card_charge_micros) * 1_000_000);
    let reload: ReloadView = null;
    if (metered && q && (savedCard || wantsCard)) {
        if (reloadNow && ar) reload = { kind: "on", amount: ar.amount, threshold: ar.threshold };
        else if (!reloadNow && ar?.enabled && ar.storeManaged) reload = { kind: "offer", amount: ar.amount };
        else if (!reloadNow && !savedCard) reload = { kind: "offer", amount: Math.max(minCharge, q.recurring_monthly_micros + (status.next_bundle?.amount_micros ?? 0)) };
    }
    const offerAutoReload = reload?.kind === "offer";

    // One key per attempt at one quote, so a double click or a retried request never orders twice.
    const attempt = React.useRef<{ quoteId: string; key: string } | null>(null);
    const keyFor = (quoteId: string) => {
        if (attempt.current?.quoteId !== quoteId) attempt.current = { quoteId, key: crypto.randomUUID() };
        return attempt.current.key;
    };

    const quoteCode = storeErrorCode(quote.error);
    const quoteProblem = quoteReady && quote.isError && !planFromQuote ? storeErrorMessage(quote.error) : null;
    const domainRefused = !!quoteCode && DOMAIN_REFUSED.has(quoteCode);
    const blocker = payBlocker({
        cartIssue,
        planNeeded: planNeeded || planFromQuote,
        canPay,
        registrantMissing: registrantNeeded && gaps.length > 0,
        cardIncomplete: showCardField && cardEntry === "inline" && !cardState.complete,
        quoteProblem: domainRefused ? quoteProblem : null,
    });
    const checklist = payChecklist({
        hasDomain: cart.domains.length > 0,
        sendersOk: !sendersIssue(cart),
        addressesOk: !addressIssue(cart),
        forwardOk: !forwardIssue(cart),
        registrantOk: registrantNeeded ? gaps.length === 0 : null,
        cardOk: showCardField && cardEntry === "inline" ? cardState.complete : null,
    });
    const label = payLabel({
        metered,
        due: plan && plan.cardCharge > 0 ? plan.cardCharge + (plan.topUpFee ?? 0) : due,
        card: savedCard,
        cardEntry: wantsCard ? cardEntry : "inline",
        fromBalance: !!plan?.balanceCovers,
    });

    // Places an order and handles every answer the same way, whether Pay was pressed or a card setup came back.
    const place = async (body: CreateOrderRequest, idempotencyKey: string) => {
        setBusy("paying");
        try {
            const placed = await createOrder.mutateAsync({ body, idempotencyKey });
            onPlaced(placed);
        } catch (e) {
            const err = e as AppError;
            const code = storeErrorCode(err);
            if (err.status && err.status < 500 && !KEY_FREED.has(err.status)) attempt.current = null;
            if (code === "payment_method_required") {
                // Stripe's own page saves the card, and the order is placed on the way back without another press.
                saveResume(body);
                setBusy("redirecting");
                toast.loading("Opening card setup. Your order waits here.", { id: "store-card" });
                if (err.setup_url) window.location.assign(err.setup_url);
                else
                    addCard.mutate(undefined, {
                        onError: () => {
                            setBusy(null);
                            toast.dismiss("store-card");
                        },
                    });
                return;
            }
            if (code === "price_changed" || code === "quote_not_found") {
                const fresh = code === "price_changed" ? repricedQuote(err) : null;
                if (fresh) qc.setQueryData(quoteQuery(items).queryKey, fresh);
                else void quote.refetch();
                setPriceNotice(true);
            } else if (code === "contact_invalid" || code === "registrant_required") {
                refuseRegistrant(err);
            } else if (code === "plan_required") {
                setOrderNeedsPlan(true);
            } else {
                setError(storeErrorMessage(err));
                if (code && DOMAIN_REFUSED.has(code)) void quote.refetch();
            }
        }
        setBusy(null);
    };

    const pay = async () => {
        if (busy) return;
        if (blocker) {
            setNudge((n) => n + 1);
            if (registrantNeeded && gaps.length > 0) setShowRegistrantErrors(true);
            return;
        }
        const shownDue = due;
        setError(null);
        setPriceNotice(false);
        setBusy("pricing");
        try {
            if (registrantNeeded) {
                try {
                    await saveRegistrant.mutateAsync(registrant);
                    savedFor.current = JSON.stringify(registrant);
                } catch (e) {
                    refuseRegistrant(e);
                    return;
                }
            }
            let fresh: Quote;
            try {
                fresh = await qc.fetchQuery(quoteQuery(items));
            } catch (e) {
                if (isPlanRequired(e)) setOrderNeedsPlan(true);
                else if (storeErrorCode(e) === "registrant_required" || storeErrorCode(e) === "contact_invalid") refuseRegistrant(e);
                else setError(storeErrorMessage(e));
                return;
            }
            // Pay what was on screen or nothing: a different total is shown and waits for another press.
            if (shownDue !== null && fresh.due_now_micros !== shownDue) {
                setPriceNotice(true);
                return;
            }
            const body: CreateOrderRequest = {
                quote_id: fresh.id,
                expected_due_now_micros: fresh.due_now_micros,
                forwarding_url: orderForwarding(cart),
                ...(offerAutoReload ? { enable_auto_reload: autoReload } : {}),
            };
            const freshPlan = paymentPlan(fresh.payment, fresh.payment.balance_micros, fresh.due_now_micros);
            const cardNeeded = !freshPlan.balanceCovers || topUp !== null;
            if (showCardField && cardEntry === "inline" && !fresh.payment.card && cardNeeded && cardRef.current) {
                setBusy("card");
                // A bank that insists on a redirect comes back to the page, which places this order then.
                saveResume(body);
                const saved = await cardRef.current.save(new URL("/app/emails/buy?payment_method=success", window.location.origin).toString());
                settleResume(false);
                if (!saved.ok) {
                    if (saved.message) setError(saved.message);
                    return;
                }
                void qc.invalidateQueries({ queryKey: [...STORE_KEY, "status"] });
            }
            if (topUp !== null && !freshPlan.balanceCovers) {
                // The round amount goes on the balance first; the order is then paid from it, at the same total.
                setBusy("card");
                try {
                    const added = await topUpBalance(topUp);
                    void qc.invalidateQueries({ queryKey: balanceQuery.queryKey });
                    if (added.status !== "paid") {
                        setError("Your payment is still going through. Press Pay again once it lands to place the order.");
                        return;
                    }
                } catch (e) {
                    setError(storeErrorMessage(e));
                    return;
                }
                setTopUpTo(null);
                try {
                    fresh = await qc.fetchQuery({ ...quoteQuery(items), staleTime: 0 });
                } catch (e) {
                    setError(storeErrorMessage(e));
                    return;
                }
                if (fresh.due_now_micros !== body.expected_due_now_micros) {
                    setPriceNotice(true);
                    return;
                }
                body.quote_id = fresh.id;
            }
            await place(body, keyFor(fresh.id));
        } finally {
            setBusy((b) => (b === "redirecting" ? b : null));
        }
    };

    // ---- back from Stripe's card page (the fallback): place the order it left to set up, at the total it showed ----
    const search = useSearch({ strict: false }) as { payment_method?: string; redirect_status?: string; setup_intent?: string };
    const handledReturn = React.useRef(false);
    React.useEffect(() => {
        if (handledReturn.current) return;
        handledReturn.current = true;
        // A bank that redirected skipped the in-page default; the card it saved becomes the default before the order is paid.
        let adopted: Promise<unknown> = Promise.resolve();
        if (search.payment_method) {
            const saved = search.payment_method === "success" && (!search.redirect_status || search.redirect_status === "succeeded");
            if (saved && search.setup_intent) adopted = adoptCardSetup(search.setup_intent).catch(() => undefined);
            toast.dismiss("store-card");
            void qc.resetQueries({ queryKey: [...STORE_KEY, "quote"] });
            void qc.invalidateQueries({ queryKey: [...STORE_KEY, "status"] });
            void qc.invalidateQueries({ queryKey: ["subscription", "credits"] });
            settleResume(saved);
            if (saved) toast.success("Card saved. Placing your order.");
            else toast("Card setup was closed. Your order is still here.");
            void navigate({ to: "/app/emails/buy", search: {}, replace: true });
        }
        const body = takeResume();
        if (body) void adopted.then(() => place(body, resumeKey(body)));
        // Once, on arrival.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    return {
        canPay,
        metered,
        quote: q,
        quoteLoading: quoteReady && !q && !quote.isError,
        quoteProblem,
        quoteDomainRefused: domainRefused,
        retryQuote: () => void quote.refetch(),
        due,
        monthly,
        yearly,
        priceNotice,
        planNeeded: planNeeded || planFromQuote,
        registrant: {
            needed: registrantNeeded,
            fields: asked,
            value: registrant,
            refused,
            showErrors: showRegistrantErrors,
            error: registrantError,
            saving: saveRegistrant.isPending,
            edit: editRegistrant,
        },
        card: {
            saved: savedCard,
            show: showCardField,
            entry: cardEntry,
            publishableKey,
            ref: cardRef,
            onState: setCardState,
        },
        plan,
        autoReload: { view: reload, value: autoReload, set: setAutoReload },
        topUp: { offer: topUpOffer, chosen: topUp, choose: setTopUpTo },
        busy,
        error,
        blocker,
        checklist,
        nudge,
        label,
        pay: () => void pay(),
    };
}

export type CheckoutState = ReturnType<typeof useCheckout>;
