// Add funds: a one-time amount or the automatic top-up, paid with the saved card or a card saved right here
// with Stripe.js. A dialog on a wide screen, a bottom sheet on a phone; Escape and a press outside close it.
import React from "react";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import { AlertTriangleIcon, ArrowRightIcon, CreditCardIcon, Loader2Icon, XIcon } from "lucide-react";
import toast from "react-hot-toast/headless";
import { Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import CardField, { type CardFieldHandle, type CardFieldState } from "@/components/app/store/CardField";
import { NumberInput } from "@/components/ui/field";
import { useQueryClient } from "@tanstack/react-query";
import type { AppError } from "@/lib/api/client/normalizeError";
import {
    RESUME_AUTO_RELOAD_KEY,
    balanceQuery,
    useAddPaymentMethod,
    useBalanceCheckout,
    useTopUp,
    useUpdateAutoReload,
} from "@/lib/api/hooks/app/subscription/useBalance";
import type { Balance } from "@/lib/api/models/app/subscription/Credits";
import buildError from "@/lib/helper/buildError";
import { EASE, PRESS_SPRING } from "@/lib/buy/motion";
import { MAX_TOP_UP_USD, TOP_UP_FEE_LABEL, fmtUsd, fmtUsdTrim, roundCents, topUpCharge } from "@/lib/money";
import { cn } from "@/lib/utils";
import { RELOAD_FAILURE_COPY, expiresSoon, fmtClock, fmtDay, paymentMethodExpiry, paymentMethodLabel } from "./balanceModel";
import { DEFAULT_FUND_USD, FUND_STEP_USD, RELOAD_THRESHOLD_MIN_USD, clampFund, lastsDays, lastsLabel, type FundsPolicy } from "./funds";

export type FundsTab = "once" | "auto";
export type FundsMode = "funds" | "card";

const LIMIT_MAX = 50_000;
const THRESHOLD_MAX = 1_000;

export default function AddFundsDialog({
    open,
    mode,
    tab,
    onTab,
    onCard,
    onClose,
    balance,
    policy,
    canBuy,
}: {
    open: boolean;
    mode: FundsMode;
    tab: FundsTab;
    onTab: (t: FundsTab) => void;
    /** Switches the dialog to replacing the card. */
    onCard: () => void;
    onClose: () => void;
    balance: Balance;
    /** The amounts offered and the least that can be added: the server's, or the store's where it is on. */
    policy: FundsPolicy;
    canBuy: boolean;
}) {
    return (
        <AnimatePresence>
            {open && <Sheet mode={mode} tab={tab} onTab={onTab} onCard={onCard} onClose={onClose} b={balance} policy={policy} canBuy={canBuy} />}
        </AnimatePresence>
    );
}

function Sheet({
    mode,
    tab,
    onTab,
    onCard,
    onClose,
    b,
    policy,
    canBuy,
}: {
    mode: FundsMode;
    tab: FundsTab;
    onTab: (t: FundsTab) => void;
    onCard: () => void;
    onClose: () => void;
    b: Balance;
    policy: FundsPolicy;
    canBuy: boolean;
}) {
    const reduced = useReducedMotion();
    const [busy, setBusy] = React.useState(false);
    const close = React.useCallback(() => {
        if (!busy) onClose();
    }, [busy, onClose]);
    React.useEffect(() => {
        const onKey = (e: KeyboardEvent) => {
            if (e.key !== "Escape") return;
            // The fresh sign-in prompt or a confirm on top takes Escape first.
            if (document.querySelector('[role="alertdialog"]')) return;
            close();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [close]);
    const title = mode === "card" ? "Payment method" : "Add funds";
    return (
        <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.15 }}
            className="fixed inset-0 z-[60] flex items-end sm:items-center justify-center bg-slate-900/30 backdrop-blur-[2px] sm:px-4"
            onMouseDown={(e) => {
                if (e.target === e.currentTarget) close();
            }}
        >
            <motion.div
                role="dialog"
                aria-modal="true"
                aria-labelledby="funds-title"
                initial={reduced ? { opacity: 0 } : { y: 24, opacity: 0 }}
                animate={{ y: 0, opacity: 1 }}
                exit={reduced ? { opacity: 0 } : { y: 24, opacity: 0 }}
                transition={{ duration: 0.22, ease: EASE }}
                className="w-full sm:max-w-[440px] max-h-[92vh] overflow-y-auto overscroll-contain rounded-t-2xl sm:rounded-2xl bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.22)] pb-[env(safe-area-inset-bottom)]"
            >
                <div className="px-5 pt-4 flex items-center gap-2">
                    <h2 id="funds-title" className="text-[15px] font-semibold tracking-[-0.01em] text-slate-900">
                        {title}
                    </h2>
                    <button
                        type="button"
                        onClick={close}
                        disabled={busy}
                        aria-label="Close"
                        className="ml-auto size-7 rounded-md text-slate-400 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors outline-none focus-visible:ring-2 focus-visible:ring-sky-200 disabled:opacity-50"
                    >
                        <XIcon className="w-3.5 h-3.5" />
                    </button>
                </div>
                {mode === "funds" && <Tabs tab={tab} onTab={onTab} />}
                {mode === "card" ? (
                    <CardOnly b={b} onBusy={setBusy} onDone={onClose} />
                ) : tab === "once" ? (
                    <OneTime b={b} policy={policy} canBuy={canBuy} onBusy={setBusy} onDone={onClose} />
                ) : (
                    <AutoTopUp b={b} policy={policy} canBuy={canBuy} onCard={onCard} onBusy={setBusy} onDone={onClose} />
                )}
            </motion.div>
        </motion.div>
    );
}

function Tabs({ tab, onTab }: { tab: FundsTab; onTab: (t: FundsTab) => void }) {
    const reduced = useReducedMotion();
    const tabs: { key: FundsTab; label: string }[] = [
        { key: "once", label: "One-time" },
        { key: "auto", label: "Auto top-up" },
    ];
    return (
        <div role="tablist" aria-label="How to add funds" className="mx-5 mt-3 grid grid-cols-2 p-0.5 rounded-lg bg-slate-100/80 border border-slate-200/70">
            {tabs.map((t, i) => {
                const on = tab === t.key;
                return (
                    <button
                        key={t.key}
                        type="button"
                        role="tab"
                        aria-selected={on}
                        tabIndex={on ? 0 : -1}
                        onClick={() => onTab(t.key)}
                        onKeyDown={(e) => {
                            if (e.key === "ArrowRight" || e.key === "ArrowLeft") {
                                e.preventDefault();
                                onTab(tabs[(i + 1) % tabs.length].key);
                            }
                        }}
                        className={cn("relative h-7 rounded-md text-[12px] font-medium outline-none focus-visible:ring-2 focus-visible:ring-sky-200 transition-colors", on ? "text-slate-900" : "text-slate-500 hover:text-slate-800")}
                    >
                        {on && (
                            <motion.span
                                layoutId={reduced ? undefined : "funds-tab"}
                                transition={PRESS_SPRING}
                                className="absolute inset-0 rounded-md bg-white shadow-[0_1px_2px_rgba(15,23,42,0.08),0_0_0_1px_rgba(15,23,42,0.04)]"
                            />
                        )}
                        <span className="relative">{t.label}</span>
                    </button>
                );
            })}
        </div>
    );
}

/** The saved card with a way to change it in place, or the card field when there is none. */
function useCard(b: Balance) {
    const pm = b.payment_method ?? null;
    const key = b.stripe_publishable_key ?? "";
    const [changing, setChanging] = React.useState(false);
    const [state, setState] = React.useState<CardFieldState>({ status: "loading", complete: false });
    const ref = React.useRef<CardFieldHandle>(null);
    const entering = (!pm || changing) && !!key && state.status !== "failed";
    return { pm, key, changing, setChanging, state, setState, ref, entering, inline: !!key && state.status !== "failed" };
}

function CardRow({ card }: { card: ReturnType<typeof useCard> }) {
    const { pm, key, changing } = card;
    if (pm && !changing) {
        const exp = paymentMethodExpiry(pm);
        return (
            <div>
                <div className="flex items-center gap-3 rounded-xl border border-slate-200 px-3.5 h-12">
                    <CreditCardIcon className="w-4 h-4 text-slate-400 shrink-0" />
                    <span className="min-w-0 flex-1 truncate text-[12.5px] text-slate-900 tabular-nums">
                        {paymentMethodLabel(pm)}
                        {exp && <span className="text-slate-400"> · {exp}</span>}
                    </span>
                    {key && (
                        <button
                            type="button"
                            onClick={() => card.setChanging(true)}
                            className="h-7 px-2 rounded-md text-[12px] font-medium text-slate-600 hover:text-slate-900 hover:bg-slate-100 transition-colors outline-none focus-visible:ring-2 focus-visible:ring-sky-200"
                        >
                            Change
                        </button>
                    )}
                </div>
                {exp && expiresSoon(pm) && <ExpiringNote exp={exp} />}
            </div>
        );
    }
    if (!key) {
        return (
            <p className="flex items-start gap-2 rounded-xl border border-dashed border-slate-200 px-3.5 py-3 text-[11.5px] text-slate-500 leading-relaxed">
                <CreditCardIcon className="w-3.5 h-3.5 mt-px text-slate-400 shrink-0" />
                Paying opens Stripe&apos;s page to add a card, then brings you back.
            </p>
        );
    }
    return (
        <div>
            <div className="mb-2 flex items-center justify-between">
                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Card</span>
                {pm && (
                    <button type="button" onClick={() => card.setChanging(false)} className="text-[11.5px] text-slate-500 hover:text-slate-900 transition-colors">
                        Keep {paymentMethodLabel(pm)}
                    </button>
                )}
            </div>
            <CardField ref={card.ref} publishableKey={key} onState={card.setState} />
        </div>
    );
}

const RETURN_TO = "/app/settings/billing/balance?payment_method=success";

/** Saves the typed card when one is being entered; true when there is a card to charge after. */
async function saveTypedCard(card: ReturnType<typeof useCard>, onError: (m: string | null) => void): Promise<boolean> {
    if (!card.entering || !card.ref.current) return true;
    const saved = await card.ref.current.save(new URL(RETURN_TO, window.location.origin).toString());
    if (!saved.ok) {
        onError(saved.message);
        return false;
    }
    return true;
}

function OneTime({ b, policy, canBuy, onBusy, onDone }: { b: Balance; policy: FundsPolicy; canBuy: boolean; onBusy: (v: boolean) => void; onDone: () => void }) {
    const qc = useQueryClient();
    const pay = useTopUp();
    const checkout = useBalanceCheckout();
    const card = useCard(b);
    const minUsd = policy.minUsd;
    const [amount, setAmount] = React.useState(policy.defaultUsd);
    const [error, setError] = React.useState<string | null>(null);
    const [saving, setSaving] = React.useState(false);
    const perDay = b.burn?.per_day_usd ?? 0;
    const inFlight = !!b.charge_in_progress;
    const busy = saving || pay.isPending || checkout.isPending;
    React.useEffect(() => onBusy(busy), [busy, onBusy]);
    const { feeUsd, totalUsd } = topUpCharge(amount);

    const reason = !canBuy
        ? "Adding funds needs an active paid plan."
        : inFlight
          ? "A payment is still going through. This updates when it lands."
          : !(amount >= minUsd && amount <= MAX_TOP_UP_USD)
            ? `Enter an amount from ${fmtUsdTrim(minUsd)} to ${fmtUsdTrim(MAX_TOP_UP_USD)}.`
            : card.entering && !card.state.complete
            ? "Enter your card details above."
            : null;

    const submit = async () => {
        if (busy || reason) return;
        setError(null);
        if (!card.pm && !card.inline) {
            checkout.mutate(amount);
            return;
        }
        setSaving(true);
        try {
            if (!(await saveTypedCard(card, setError))) return;
            if (card.entering) void qc.invalidateQueries({ queryKey: balanceQuery.queryKey });
            const r = await pay.mutateAsync(amount).catch(() => undefined);
            if (r) onDone();
        } finally {
            setSaving(false);
        }
    };

    return (
        <div className="px-5 pt-4 pb-5">
            <div role="radiogroup" aria-label="Amount" className="grid grid-cols-3 gap-2">
                {policy.presetsUsd.map((p) => {
                    const on = amount === p;
                    const days = lastsDays(b.balance_usd + p, perDay);
                    return (
                        <button
                            key={p}
                            type="button"
                            role="radio"
                            aria-checked={on}
                            onClick={() => setAmount(p)}
                            className={cn(
                                "h-14 rounded-xl border text-center outline-none focus-visible:ring-2 focus-visible:ring-sky-200 transition-colors",
                                on ? "border-sky-400 bg-sky-50/70 shadow-[0_0_0_1px_rgba(56,189,248,0.5)]" : "border-slate-200 bg-white hover:border-slate-300",
                            )}
                        >
                            <span className={cn("block text-[14px] font-semibold tabular-nums", on ? "text-sky-800" : "text-slate-900")}>{fmtUsdTrim(p)}</span>
                            <span className="block text-[10.5px] text-slate-400 tabular-nums">{days === null ? " " : days > 365 ? "over a year" : `~${days} days`}</span>
                        </button>
                    );
                })}
            </div>

            <label htmlFor="funds-custom" className="mt-4 block mb-1.5 text-[11.5px] text-slate-500">
                Or enter a custom amount
            </label>
            <NumberInput
                id="funds-custom"
                value={amount}
                onChange={(v) => setAmount(v)}
                onCommit={(v) => setAmount(clampFund(v, minUsd))}
                min={minUsd}
                max={MAX_TOP_UP_USD}
                step={FUND_STEP_USD}
                suffix="USD"
                align="center"
                className="w-full h-10 text-[15px]"
            />
            <p className="mt-1 text-[11px] text-slate-400">
                From {fmtUsdTrim(minUsd)} to {fmtUsdTrim(MAX_TOP_UP_USD)}, in steps of {fmtUsdTrim(FUND_STEP_USD)} with the arrows.
            </p>

            <div className="mt-4 rounded-xl bg-slate-50 border border-slate-100 px-3.5 h-11 flex items-center justify-between gap-3">
                <span className="text-[12.5px] font-medium text-slate-800">{lastsLabel(lastsDays(b.balance_usd + amount, perDay))}</span>
                {perDay > 0 && <span className="text-[11.5px] text-slate-400 tabular-nums">{fmtUsd(perDay)}/day avg</span>}
            </div>

            <div className="mt-3">
                <CardRow card={card} />
            </div>

            {error && (
                <p role="alert" className="mt-3 rounded-lg border border-red-200 bg-red-50/70 px-3 py-2 text-[11.5px] text-red-800 leading-relaxed">
                    {error}
                </p>
            )}

            <div className="mt-5 -mx-5 px-5 pt-4 border-t border-slate-100 space-y-1 text-[12px] tabular-nums">
                <div className="flex items-baseline justify-between text-slate-500">
                    <span>Added to balance</span>
                    <span className="text-slate-700">{fmtUsd(amount)}</span>
                </div>
                <div className="flex items-baseline justify-between text-slate-500">
                    <span>{TOP_UP_FEE_LABEL}</span>
                    <span className="text-slate-700">{fmtUsd(feeUsd)}</span>
                </div>
            </div>
            <div className="mt-2 flex items-baseline justify-between">
                <span className="text-[12.5px] text-slate-500">Total</span>
                <span className="text-[22px] font-semibold tracking-[-0.02em] text-slate-900 tabular-nums">{fmtUsd(totalUsd)}</span>
            </div>
            <PrimaryButton busy={busy} blocked={!!reason} onClick={() => void submit()}>
                {busy ? (saving && card.entering ? "Saving your card…" : "Paying…") : card.pm || card.inline ? `Pay ${fmtUsd(totalUsd)}` : `Continue to pay ${fmtUsd(totalUsd)}`}
            </PrimaryButton>
            {card.pm && !card.entering && (
                <button
                    type="button"
                    onClick={() => {
                        if (!busy && !reason) checkout.mutate(amount);
                    }}
                    aria-disabled={busy || !!reason || undefined}
                    className="mt-2 w-full h-8 rounded-lg text-[12px] font-medium text-slate-600 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center gap-1.5 transition-colors outline-none focus-visible:ring-2 focus-visible:ring-sky-200 aria-disabled:opacity-50 aria-disabled:cursor-not-allowed"
                >
                    {checkout.isPending && <Loader2Icon className="w-3 h-3 animate-spin" />}
                    Pay another way
                </button>
            )}
            <p className="mt-2 text-center text-[11px] text-slate-400" role={reason ? "status" : undefined}>
                {reason ?? "Never expires. You may be asked to confirm it is you."}
            </p>
        </div>
    );
}

function AutoTopUp({
    b,
    policy,
    canBuy,
    onCard,
    onBusy,
    onDone,
}: {
    b: Balance;
    policy: FundsPolicy;
    canBuy: boolean;
    onCard: () => void;
    onBusy: (v: boolean) => void;
    onDone: () => void;
}) {
    const qc = useQueryClient();
    const save = useUpdateAutoReload();
    const addCard = useAddPaymentMethod();
    const card = useCard(b);
    const ar = b.auto_reload;
    const minUsd = policy.minUsd;
    const bundle = Math.ceil(b.burn?.mailboxes_monthly_usd ?? 0);
    // Turned on for the first time: the server's saved defaults, or the store's larger amounts where it is on.
    const fresh = !ar?.enabled && policy.store;
    const [enabled, setEnabled] = React.useState(ar?.enabled ?? true);
    const [threshold, setThreshold] = React.useState(() => (fresh ? Math.max(minUsd, bundle) : (ar?.threshold_usd ?? 5)));
    const [amount, setAmount] = React.useState(() => (fresh ? DEFAULT_FUND_USD : (ar?.amount_usd ?? policy.defaultUsd)));
    const [limit, setLimit] = React.useState(() => (fresh ? Math.max(ar?.monthly_limit_usd ?? 0, DEFAULT_FUND_USD * 5) : (ar?.monthly_limit_usd ?? 250)));
    const [error, setError] = React.useState<string | null>(null);
    const [saving, setSaving] = React.useState(false);
    const busy = saving || save.isPending || addCard.isPending;
    React.useEffect(() => onBusy(busy), [busy, onBusy]);
    const charge = topUpCharge(amount);

    const needsCard = enabled && !card.pm;
    // Only turning it on needs a paid plan; a lapsed workspace can always turn it off.
    const reason = enabled && !canBuy
        ? "Auto top-up needs an active paid plan. You can still turn it off."
        : enabled && threshold < RELOAD_THRESHOLD_MIN_USD
          ? `Use at least ${fmtUsd(RELOAD_THRESHOLD_MIN_USD)} for the balance it tops up at.`
          : enabled && amount < minUsd
          ? `Add at least ${fmtUsdTrim(minUsd)} each time.`
          : enabled && amount <= threshold
          ? "Add more than the balance it tops up at."
          : enabled && limit < amount
            ? "The monthly limit has to cover at least one top-up."
            : needsCard && card.entering && !card.state.complete
              ? "Enter your card details above."
              : null;

    const submit = async () => {
        if (busy || reason) return;
        setError(null);
        const patch = { enabled, threshold_usd: threshold, amount_usd: amount, monthly_limit_usd: limit };
        if (needsCard && !card.inline) {
            try {
                sessionStorage.setItem(RESUME_AUTO_RELOAD_KEY, JSON.stringify(patch));
            } catch {
                // Private mode: turned on by hand once the card is saved.
            }
            addCard.mutate();
            return;
        }
        setSaving(true);
        try {
            if (needsCard && !(await saveTypedCard(card, setError))) return;
            await save.mutateAsync(patch);
            void qc.invalidateQueries({ queryKey: balanceQuery.queryKey });
            toast.success(enabled ? "Auto top-up is on" : "Auto top-up is off");
            onDone();
        } catch (e) {
            setError(buildError(e as AppError));
        } finally {
            setSaving(false);
        }
    };

    const failureCode = ar && !ar.enabled ? ar.failure_code : null;
    const failure = failureCode ? RELOAD_FAILURE_COPY[failureCode] : null;
    const retryAt = ar?.enabled && ar.retry_at ? new Date(ar.retry_at) : null;
    const last = ar?.last_reload ?? null;
    const cardLabel = card.pm ? "Update card" : "Add a card";
    return (
        <div className="px-5 pt-4 pb-5">
            {failure && (
                <Notice action={failureCode !== "unavailable" ? { label: cardLabel, onClick: onCard } : undefined}>{failure}</Notice>
            )}
            {retryAt && (
                <Notice action={{ label: "Update card", onClick: onCard }}>
                    The bank declined the last top-up for now. {retryLine(retryAt)}
                </Notice>
            )}
            <div className="flex items-start gap-3">
                <div className="min-w-0 flex-1">
                    <p className="text-[12.5px] font-medium text-slate-900">Keep the balance topped up</p>
                    <p className="mt-0.5 text-[11.5px] text-slate-500 leading-relaxed">Adds money on its own when the balance runs low, so mailboxes and AI never pause.</p>
                </div>
                <Toggle value={enabled} onChange={setEnabled} ariaLabel="Auto top-up" />
            </div>
            <div className={cn("mt-4 grid grid-cols-2 gap-x-3 gap-y-3 transition-opacity", !enabled && "opacity-50")}>
                <AmountField id="auto-threshold" label="When balance drops below" value={threshold} onChange={setThreshold} min={RELOAD_THRESHOLD_MIN_USD} max={THRESHOLD_MAX} cents disabled={!enabled} />
                <AmountField id="auto-amount" label="Add" value={amount} onChange={setAmount} min={minUsd} max={MAX_TOP_UP_USD} disabled={!enabled} />
                <div className="col-span-2">
                    <AmountField id="auto-limit" label="At most a month" value={limit} onChange={setLimit} min={Math.max(minUsd, amount)} max={LIMIT_MAX} disabled={!enabled} />
                </div>
            </div>
            {enabled && amount > 0 && (
                <p className="mt-2 text-[11px] text-slate-400 tabular-nums">
                    {TOP_UP_FEE_LABEL} {fmtUsd(charge.feeUsd)} · Charged {fmtUsd(charge.totalUsd)} each top-up
                </p>
            )}
            {bundle > 0 && enabled && (
                <p className="mt-2 text-[11px] text-slate-400">Next month&apos;s mailboxes come to {fmtUsdTrim(bundle)}, so a threshold at or above it keeps them paid from the balance.</p>
            )}
            {needsCard && (
                <div className="mt-4">
                    <CardRow card={card} />
                </div>
            )}
            {ar && (last || ar.enabled) && (
                <div className="mt-3 flex flex-wrap items-center justify-between gap-x-4 gap-y-1 text-[11.5px]">
                    <span className="min-w-0 text-slate-500">{last ? `Last top-up ${fmtDay(last.at)}: ${fmtUsd(last.amount_usd)}` : "No top-ups yet"}</span>
                    {ar.enabled && (
                        <span className="shrink-0 tabular-nums text-slate-400">
                            {fmtUsd(ar.reloaded_this_month_usd)} of {fmtUsdTrim(ar.monthly_limit_usd)} this month
                        </span>
                    )}
                </div>
            )}
            {error && (
                <p role="alert" className="mt-3 rounded-lg border border-red-200 bg-red-50/70 px-3 py-2 text-[11.5px] text-red-800 leading-relaxed">
                    {error}
                </p>
            )}
            <div className="mt-5">
                <PrimaryButton busy={busy} blocked={!!reason} onClick={() => void submit()}>
                    {busy ? "Saving…" : "Save"}
                </PrimaryButton>
            </div>
            {reason && (
                <p role="status" className="mt-2 text-center text-[11px] text-slate-500">
                    {reason}
                </p>
            )}
        </div>
    );
}

function CardOnly({ b, onBusy, onDone }: { b: Balance; onBusy: (v: boolean) => void; onDone: () => void }) {
    const qc = useQueryClient();
    const addCard = useAddPaymentMethod();
    const card = useCard(b);
    const [error, setError] = React.useState<string | null>(null);
    const [saving, setSaving] = React.useState(false);
    const busy = saving || addCard.isPending;
    React.useEffect(() => onBusy(busy), [busy, onBusy]);
    const submit = async () => {
        if (busy) return;
        setError(null);
        if (!card.inline) {
            addCard.mutate();
            return;
        }
        setSaving(true);
        try {
            if (!(await saveTypedCard(card, setError))) return;
            void qc.invalidateQueries({ queryKey: balanceQuery.queryKey });
            toast.success("Card saved");
            onDone();
        } finally {
            setSaving(false);
        }
    };
    const showForm = card.entering || !card.pm || !card.inline;
    return (
        <div className="px-5 pt-4 pb-5">
            <p className="mb-3 text-[11.5px] text-slate-500 leading-relaxed">Used for adding funds and the automatic top-up. Stripe keeps the card; Warmbly never sees the number.</p>
            <CardRow card={card} />
            {error && (
                <p role="alert" className="mt-3 rounded-lg border border-red-200 bg-red-50/70 px-3 py-2 text-[11.5px] text-red-800 leading-relaxed">
                    {error}
                </p>
            )}
            {showForm && (
                <div className="mt-5">
                    <PrimaryButton busy={busy} blocked={card.entering && !card.state.complete} onClick={() => void submit()}>
                        {busy ? "Saving your card…" : card.inline ? "Save card" : "Add a card on Stripe"}
                    </PrimaryButton>
                </div>
            )}
        </div>
    );
}

function AmountField({
    id,
    label,
    value,
    onChange,
    min,
    max,
    cents,
    disabled,
}: {
    id: string;
    label: string;
    value: number;
    onChange: (v: number) => void;
    min: number;
    max: number;
    /** Keeps cents instead of rounding to whole dollars. */
    cents?: boolean;
    disabled?: boolean;
}) {
    const commit = (v: number) => onChange(cents ? Math.min(max, Math.max(min, Number.isFinite(v) ? roundCents(v) : min)) : clampFund(v, min));
    return (
        <div className="min-w-0">
            <label htmlFor={id} className="block mb-1 text-[11px] text-slate-500">
                {label}
            </label>
            <NumberInput id={id} value={value} onChange={onChange} onCommit={commit} min={min} max={max} step={cents ? 1 : FUND_STEP_USD} suffix="USD" disabled={disabled} className="w-full h-8" />
        </div>
    );
}

function PrimaryButton({ busy, blocked, onClick, children }: { busy: boolean; blocked: boolean; onClick: () => void; children: React.ReactNode }) {
    const reduced = useReducedMotion();
    const live = !reduced && !busy && !blocked;
    return (
        <motion.button
            type="button"
            onClick={onClick}
            aria-disabled={busy || blocked || undefined}
            whileHover={live ? { boxShadow: "0 10px 22px -10px rgba(2,132,199,0.6)" } : undefined}
            whileTap={live ? { scale: 0.97 } : undefined}
            transition={PRESS_SPRING}
            className={cn(
                "relative overflow-hidden mt-3 w-full h-11 rounded-xl bg-sky-600 text-white text-[13.5px] font-semibold inline-flex items-center justify-center gap-2 outline-none focus-visible:ring-2 focus-visible:ring-sky-300 focus-visible:ring-offset-2 transition-[background-color,opacity]",
                blocked ? "opacity-55 cursor-not-allowed" : "hover:bg-sky-700",
                busy && "cursor-wait",
            )}
        >
            {busy && (
                <span aria-hidden="true" className="absolute inset-0 overflow-hidden">
                    <span className="absolute inset-y-0 -left-1/2 w-1/2 progress-sweep bg-gradient-to-r from-transparent via-white/30 to-transparent" />
                </span>
            )}
            {busy && <Loader2Icon className="w-4 h-4 animate-spin" />}
            <span className="relative tabular-nums">{children}</span>
            {!busy && <ArrowRightIcon className="relative w-4 h-4" />}
        </motion.button>
    );
}

function retryLine(at: Date): string {
    const when = at.toDateString() === new Date().toDateString() ? `at ${fmtClock(at)}` : `${fmtDay(at)} at ${fmtClock(at)}`;
    return `It is tried again ${when}. Auto top-up stays on; update the card if it has changed.`;
}

function ExpiringNote({ exp }: { exp: string }) {
    return (
        <p className="mt-1.5 flex items-center gap-1 text-[11px] text-amber-700">
            <AlertTriangleIcon className="w-3 h-3 shrink-0" />
            Expires {exp}. Replace it before a top-up fails.
        </p>
    );
}

function Notice({ action, children }: { action?: { label: string; onClick: () => void }; children: React.ReactNode }) {
    return (
        <div role="status" className="mb-3 flex items-start gap-2 rounded-lg border border-amber-200 bg-amber-50/70 px-3 py-2 text-[11.5px] text-amber-900 leading-relaxed">
            <AlertTriangleIcon className="w-3.5 h-3.5 mt-px shrink-0" />
            <span className="min-w-0 flex-1">{children}</span>
            {action && (
                <button
                    type="button"
                    onClick={action.onClick}
                    className="shrink-0 h-6 px-2 rounded-md border border-amber-300 bg-white text-[11.5px] font-medium text-amber-900 hover:bg-amber-50 transition-colors outline-none focus-visible:ring-2 focus-visible:ring-amber-200"
                >
                    {action.label}
                </button>
            )}
        </div>
    );
}
