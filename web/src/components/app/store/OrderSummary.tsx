// The order summary: what is being bought, due today, how the months after are paid, and one Pay button.
// A sticky card beside the order on a wide screen; a bottom bar that opens into the same card on a phone.
import React from "react";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import { AlertCircleIcon, ArrowRightIcon, ChevronUpIcon, InfoIcon, Loader2Icon, LockIcon, RefreshCwIcon, TrendingUpIcon } from "lucide-react";
import ProviderLogo from "@/components/app/emails/ProviderLogo";
import { Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import { FieldError, TextInput } from "@/components/ui/field";
import RollingNumber from "@/components/ui/RollingNumber";
import type { QuoteLine, StoreStatus } from "@/lib/api/models/app/store/Store";
import { domainMailboxes, effectiveProvider, mailboxCount, type Cart, type PriceBook } from "@/lib/buy/cart";
import { autoReloadLine, balanceLine, monthlyLine, type PayStep } from "@/lib/buy/checkout";
import { fmtMicros, fmtMonthly, fmtYearly } from "@/lib/buy/money";
import { fmtUsd, fmtUsdTrim, topUpCharge } from "@/lib/money";
import { EASE, PRESS_SPRING } from "@/lib/buy/motion";
import { normalizePhone, REGISTRANT_LABELS, registrantProblems, type RegistrantField } from "@/lib/buy/registrant";
import { cn } from "@/lib/utils";
import CardField from "./CardField";
import PlanUpsell from "./PlanUpsell";
import type { Busy, CheckoutState } from "./useCheckout";

const REGISTRANT_AUTOCOMPLETE: Partial<Record<RegistrantField, string>> = {
    first_name: "given-name",
    last_name: "family-name",
    organization: "organization",
    email: "email",
    phone: "tel",
    address1: "address-line1",
    address2: "address-line2",
    city: "address-level2",
    state: "address-level1",
    postal_code: "postal-code",
    country: "country",
};
const REGISTRANT_PLACEHOLDER: Partial<Record<RegistrantField, string>> = { phone: "+1.4155550100", country: "US" };
const WIDE: RegistrantField[] = ["organization", "email", "address1", "address2"];

interface Props {
    cart: Cart;
    status: StoreStatus;
    prices: PriceBook;
    checkout: CheckoutState;
}

/** The desktop card: sticky beside the order, and the shape the setup track grows out of after Pay. */
export function SummaryPanel(props: Props) {
    const reduced = useReducedMotion();
    return (
        <motion.section
            layoutId={reduced ? undefined : "buy-panel"}
            aria-label="Order summary"
            className="rounded-2xl border border-slate-200 bg-white shadow-[0_1px_2px_rgba(15,23,42,0.04),0_16px_40px_-24px_rgba(15,23,42,0.28)] overflow-hidden"
        >
            <div className="px-4 pt-4">
                <h2 className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Order summary</h2>
            </div>
            <SummaryBody {...props} />
            <div className="px-4 pb-4 pt-1">
                <PayArea checkout={props.checkout} />
            </div>
        </motion.section>
    );
}

/** The phone bar: the total and Pay, opening upward into the full summary. */
export function SummarySheet(props: Props) {
    const reduced = useReducedMotion();
    const { checkout } = props;
    const [open, setOpen] = React.useState(false);
    const detailsId = "buy-summary-details";
    React.useEffect(() => {
        if (!open) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key === "Escape") setOpen(false);
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [open]);
    // A blocked Pay opens the summary, where the reason and the fields to fill sit.
    const lastNudge = React.useRef(checkout.nudge);
    React.useEffect(() => {
        if (checkout.nudge !== lastNudge.current) {
            lastNudge.current = checkout.nudge;
            setOpen(true);
        }
    }, [checkout.nudge]);
    const boxes = mailboxCount(props.cart);
    return (
        <>
            <AnimatePresence>
                {open && (
                    <motion.div
                        key="scrim"
                        aria-hidden="true"
                        initial={{ opacity: 0 }}
                        animate={{ opacity: 1 }}
                        exit={{ opacity: 0 }}
                        onMouseDown={() => setOpen(false)}
                        className="fixed inset-0 z-30 bg-slate-900/25"
                    />
                )}
            </AnimatePresence>
            <section aria-label="Order summary" className="fixed inset-x-0 bottom-0 z-40 rounded-t-2xl border-t border-slate-200 bg-white shadow-[0_-12px_32px_-16px_rgba(15,23,42,0.3)]">
                {/* Kept mounted while closed, so a card typed into the field is still there to pay with. */}
                <motion.div
                    id={detailsId}
                    initial={false}
                    animate={{ height: open ? "auto" : 0, opacity: open ? 1 : 0 }}
                    transition={reduced ? { duration: 0 } : { duration: 0.26, ease: EASE }}
                    className="overflow-hidden"
                    aria-hidden={!open}
                    inert={!open}
                >
                    <div className="max-h-[62vh] overflow-y-auto overscroll-contain pt-3">
                        <SummaryBody {...props} />
                    </div>
                </motion.div>
                <div className="px-4 pt-2.5 pb-[max(0.625rem,env(safe-area-inset-bottom))] border-t border-slate-100">
                    <div className="flex items-center gap-3">
                        <button
                            type="button"
                            onClick={() => setOpen((v) => !v)}
                            aria-expanded={open}
                            aria-controls={detailsId}
                            className="min-w-0 flex-1 text-left rounded-md outline-none focus-visible:ring-2 focus-visible:ring-sky-200"
                        >
                            <span className="flex items-center gap-1 text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                                Due today
                                <ChevronUpIcon className={cn("w-3 h-3 transition-transform", !open && "rotate-180")} />
                            </span>
                            <span className="block text-[18px] font-semibold tracking-[-0.02em] text-slate-900 tabular-nums leading-tight">
                                {checkout.due === null ? <span className="inline-block h-5 w-20 rounded skeleton-shimmer align-middle" /> : <RollingNumber value={checkout.due} format={fmtMicros} />}
                            </span>
                            <span className="block text-[11px] text-slate-500 truncate">
                                {props.cart.domains.length === 0
                                    ? "No domains yet"
                                    : `${props.cart.domains.length} ${props.cart.domains.length === 1 ? "domain" : "domains"} · ${boxes} ${boxes === 1 ? "mailbox" : "mailboxes"}`}
                            </span>
                        </button>
                        {!checkout.planNeeded && <PayButton checkout={checkout} compact />}
                    </div>
                    {!open && checkout.blocker && (
                        <p className="mt-1.5 text-[11px] text-slate-500 leading-snug">{checkout.checklist.find((c) => !c.done)?.label ?? checkout.blocker}</p>
                    )}
                </div>
            </section>
        </>
    );
}

function SummaryBody({ cart, status, prices, checkout }: Props) {
    const reduced = useReducedMotion();
    const plan = checkout.plan;
    const payer = checkout.card.saved ?? (checkout.card.show || (plan && !plan.balanceCovers) ? "new_card" : "balance");
    const monthly = monthlyLine(checkout.monthly, checkout.metered, payer);
    const paidHow = plan ? balanceLine(plan, checkout.due) : null;
    return (
        <div>
            <AnimatePresence initial={false}>
                {checkout.priceNotice && (
                    <motion.div
                        key="repriced"
                        role="status"
                        initial={reduced ? { opacity: 0 } : { opacity: 0, height: 0 }}
                        animate={{ opacity: 1, height: "auto" }}
                        exit={reduced ? { opacity: 0 } : { opacity: 0, height: 0 }}
                        transition={{ duration: 0.22, ease: EASE }}
                        className="overflow-hidden"
                    >
                        <div className="mx-4 mt-3 rounded-lg border border-sky-200 bg-sky-50/70 px-3 py-2 flex items-start gap-2">
                            <InfoIcon className="w-3.5 h-3.5 text-sky-600 mt-px shrink-0" />
                            <p className="text-[11.5px] text-sky-900 leading-relaxed">Prices updated. Check the new total, then pay when it looks right.</p>
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>

            <Lines cart={cart} prices={prices} checkout={checkout} monthlyMicros={status.mailbox_monthly_micros} />
            {cart.domains.length > 0 && (
                <p className="px-4 pb-2.5 -mt-0.5 flex items-center gap-1.5 text-[11px] text-slate-500 leading-snug">
                    <TrendingUpIcon className="w-3 h-3 text-emerald-600 shrink-0" />
                    New mailboxes start sending gently and ramp up automatically.
                </p>
            )}

            {checkout.quoteProblem && (
                <div className="mx-4 mb-3 rounded-lg border border-amber-200 bg-amber-50/60 px-3 py-2 flex items-start gap-2 text-[11.5px] text-amber-900">
                    <AlertCircleIcon className="w-3.5 h-3.5 mt-px shrink-0" />
                    <span className="min-w-0 flex-1 leading-relaxed">{checkout.quoteProblem}</span>
                    {!checkout.quoteDomainRefused && (
                        <button type="button" onClick={checkout.retryQuote} className="inline-flex items-center gap-1 font-medium shrink-0">
                            <RefreshCwIcon className="w-3 h-3" /> Try again
                        </button>
                    )}
                </div>
            )}

            <div className="px-4 py-3.5 border-t border-slate-100 bg-slate-50/60">
                <div className="flex items-end justify-between gap-3">
                    <span className="pb-1 text-[10px] uppercase tracking-[0.14em] text-slate-500 font-medium">Due today</span>
                    <span className={cn("text-[26px] font-semibold tracking-[-0.02em] text-slate-900 tabular-nums leading-none transition-opacity", checkout.quoteLoading && "opacity-60")}>
                        {checkout.due === null ? (
                            cart.domains.length === 0 ? (
                                fmtMicros(0)
                            ) : (
                                <span className="inline-block h-6 w-24 rounded skeleton-shimmer align-bottom" aria-label="Pricing" />
                            )
                        ) : (
                            <RollingNumber value={checkout.due} format={fmtMicros} />
                        )}
                    </span>
                </div>
                {cart.domains.length > 0 && (
                    <div className="mt-2 space-y-1">
                        {checkout.metered ? (
                            <>
                                {monthly && <p className="text-[12px] text-slate-700 leading-snug">{monthly}</p>}
                                {paidHow && <p className="text-[11px] text-slate-500 leading-snug">{paidHow}</p>}
                                {checkout.topUp.offer !== null && (
                                    <button
                                        type="button"
                                        onClick={() => checkout.topUp.choose(checkout.topUp.chosen === null ? checkout.topUp.offer : null)}
                                        aria-pressed={checkout.topUp.chosen !== null}
                                        className={cn(
                                            "mt-0.5 h-6 px-2 rounded-full border text-[11px] font-medium inline-flex items-center gap-1 outline-none focus-visible:ring-2 focus-visible:ring-sky-200 transition-colors",
                                            checkout.topUp.chosen !== null
                                                ? "border-sky-300 bg-sky-50 text-sky-800"
                                                : "border-slate-200 bg-white text-slate-600 hover:border-slate-300 hover:text-slate-900",
                                        )}
                                    >
                                        {checkout.topUp.chosen !== null
                                            ? `Adding ${fmtUsdTrim(checkout.topUp.offer)} + ${fmtUsd(topUpCharge(checkout.topUp.offer).feeUsd)} fee · Undo`
                                            : `Add ${fmtUsdTrim(checkout.topUp.offer)} instead (+3% fee)`}
                                    </button>
                                )}
                                {checkout.autoReload.view?.kind === "offer" ? (
                                    <div className="pt-1 flex items-start gap-2.5">
                                        <Toggle value={checkout.autoReload.value} onChange={checkout.autoReload.set} ariaLabel="Keep my balance topped up" />
                                        <span className="text-[11px] text-slate-600 leading-snug">{autoReloadLine(checkout.autoReload.view)}</span>
                                    </div>
                                ) : checkout.autoReload.view?.kind === "on" ? (
                                    <p className="text-[11px] text-slate-500 leading-snug">{autoReloadLine(checkout.autoReload.view)}</p>
                                ) : null}
                            </>
                        ) : (
                            <p className="text-[11.5px] text-slate-500 leading-snug">Nothing is charged to this workspace. This instance&apos;s operator covers purchases.</p>
                        )}
                    </div>
                )}
            </div>

            {checkout.planNeeded ? (
                <div className="p-4">
                    <PlanUpsell compact />
                </div>
            ) : (
                <>
                    <RegistrantPrompt checkout={checkout} />
                    {checkout.card.show && (
                        <div className="px-4 pt-3.5">
                            <div className="mb-2 flex items-center justify-between gap-2">
                                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Card</span>
                                <span className="inline-flex items-center gap-1 text-[11px] text-slate-400">
                                    <LockIcon className="w-3 h-3" /> Saved for the monthly charges
                                </span>
                            </div>
                            <CardField ref={checkout.card.ref} publishableKey={checkout.card.publishableKey} onState={checkout.card.onState} />
                        </div>
                    )}
                    <AnimatePresence initial={false}>
                        {checkout.error && (
                            <motion.p
                                key={checkout.error}
                                role="alert"
                                initial={{ opacity: 0, y: -2 }}
                                animate={{ opacity: 1, y: 0 }}
                                exit={{ opacity: 0 }}
                                className="mx-4 mt-3 rounded-lg border border-red-200 bg-red-50/70 px-3 py-2 flex items-start gap-2 text-[11.5px] text-red-800 leading-relaxed"
                            >
                                <AlertCircleIcon className="w-3.5 h-3.5 mt-px shrink-0" />
                                {checkout.error}
                            </motion.p>
                        )}
                    </AnimatePresence>
                    <div className="h-3" />
                </>
            )}
        </div>
    );
}

function Lines({ cart, prices, checkout, monthlyMicros }: { cart: Cart; prices: PriceBook; checkout: CheckoutState; monthlyMicros: number }) {
    const reduced = useReducedMotion();
    const q = checkout.quote;
    // Live search prices draw the lines straight away; the quote's amounts replace them when it lands.
    const find = (type: QuoteLine["type"], domain: string) => q?.lines.find((l) => l.type === type && l.domain === domain);
    return (
        <div className="px-4">
            <AnimatePresence initial={false} mode="popLayout">
                {cart.domains.length === 0 && (
                    <motion.p
                        key="empty"
                        initial={{ opacity: 0 }}
                        animate={{ opacity: 1 }}
                        exit={{ opacity: 0, transition: { duration: 0.1 } }}
                        className="pt-2 pb-4 text-[12px] text-slate-500 leading-relaxed"
                    >
                        Nothing here yet. Search for a domain and add it to start the order.
                    </motion.p>
                )}
            </AnimatePresence>
            <ul className="pt-1 pb-2">
                <AnimatePresence initial={false}>
                    {cart.domains.map((d, i) => {
                        const dl = find("domain", d.domain);
                        const ml = find("mailboxes", d.domain);
                        const live = prices[d.domain];
                        const domainAmount = dl?.amount_micros ?? live?.price_micros ?? null;
                        const renew = dl?.type === "domain" ? dl.renew_micros : (live?.renew_micros ?? null);
                        const count = ml?.type === "mailboxes" ? ml.count : domainMailboxes(cart, d).length;
                        const each = ml?.type === "mailboxes" ? ml.monthly_micros : monthlyMicros;
                        const years = dl?.type === "domain" ? dl.years : d.years;
                        const nsChange = ml?.type === "mailboxes" && ml.nameservers_change;
                        return (
                            <motion.li
                                key={d.domain}
                                layout={reduced ? false : "position"}
                                initial={reduced ? { opacity: 0 } : { opacity: 0, height: 0, x: 12 }}
                                animate={{ opacity: 1, height: "auto", x: 0 }}
                                exit={reduced ? { opacity: 0 } : { opacity: 0, height: 0, x: -8 }}
                                transition={reduced ? { duration: 0.15 } : { duration: 0.28, ease: EASE }}
                                className={cn("overflow-hidden", i > 0 && "border-t border-slate-100")}
                            >
                                <div className="py-2.5">
                                    <div className="flex items-baseline justify-between gap-3">
                                        <span className="min-w-0 [overflow-wrap:anywhere] text-[12.5px] font-medium text-slate-900">{d.domain}</span>
                                        {d.kind === "owned" ? <span className="text-[11.5px] text-slate-400 shrink-0">Yours</span> : <Amount micros={domainAmount} pending={checkout.quoteLoading} />}
                                    </div>
                                    {d.kind === "new" && (
                                        <div className="text-[11px] text-slate-500">
                                            {years} {years === 1 ? "year" : "years"}
                                            {renew !== null ? `, renews at ${fmtYearly(renew)}` : ""}
                                        </div>
                                    )}
                                    <div className="mt-1 flex items-center justify-between gap-3">
                                        <span className="min-w-0 flex items-center gap-1.5 text-[12px] text-slate-600">
                                            <ProviderLogo id={effectiveProvider(cart, d)} size="xs" framed={false} />
                                            <span className="truncate tabular-nums">
                                                <RollingNumber value={count} /> {count === 1 ? "mailbox" : "mailboxes"} × {fmtMonthly(each)}
                                            </span>
                                        </span>
                                        <Amount micros={ml?.amount_micros ?? count * each} pending={checkout.quoteLoading} />
                                    </div>
                                    {nsChange && (
                                        <p className="mt-1.5 text-[11px] text-sky-800 leading-relaxed">
                                            After the order, set new nameservers for {d.domain} at its registrar. They are on the domain under Domains, Purchased.
                                        </p>
                                    )}
                                </div>
                            </motion.li>
                        );
                    })}
                </AnimatePresence>
            </ul>
        </div>
    );
}

function Amount({ micros, pending }: { micros: number | null; pending: boolean }) {
    if (micros === null) return <span className="h-3.5 w-14 rounded skeleton-shimmer shrink-0" aria-label="Pricing" />;
    return (
        <span className={cn("text-[12.5px] text-slate-900 tabular-nums font-medium shrink-0 transition-opacity", pending && "opacity-50")}>
            <RollingNumber value={micros} format={fmtMicros} />
        </span>
    );
}

/** Shown only when the registry needs something the billing details and the account did not give. */
function RegistrantPrompt({ checkout }: { checkout: CheckoutState }) {
    const reduced = useReducedMotion();
    const r = checkout.registrant;
    const show = r.needed && (r.fields.length > 0 || !!r.error);
    const problems = registrantProblems(r.value);
    return (
        <AnimatePresence initial={false}>
            {show && (
                <motion.div
                    key="registrant"
                    initial={reduced ? { opacity: 0 } : { opacity: 0, height: 0 }}
                    animate={{ opacity: 1, height: "auto" }}
                    exit={reduced ? { opacity: 0 } : { opacity: 0, height: 0 }}
                    transition={{ duration: 0.24, ease: EASE }}
                    className="overflow-hidden"
                >
                    <fieldset className="mx-4 mt-3.5 rounded-xl border border-amber-200 bg-amber-50/40 p-3">
                        <legend className="sr-only">Domain registrant</legend>
                        <p className="text-[12px] font-medium text-slate-900">
                            {r.fields.length === 1 ? "The registry needs one more detail" : "The registry needs a few more details"}
                        </p>
                        <p className="mt-0.5 text-[11px] text-slate-600 leading-relaxed">
                            Every domain is registered to a person with a postal address. Privacy keeps them out of public lookups wherever the extension allows it.
                        </p>
                        {r.error && <p className="mt-1.5 text-[11px] text-red-600">{r.error}</p>}
                        <div className="mt-2.5 grid grid-cols-2 gap-x-2 gap-y-2">
                            {r.fields.map((f) => {
                                const id = `registrant-${f}`;
                                const err = (r.showErrors ? problems[f] : undefined) ?? (r.refused.includes(f) ? "Not accepted. Check this." : undefined);
                                return (
                                    <div key={f} className={cn("min-w-0", (WIDE.includes(f) || r.fields.length === 1) && "col-span-2")}>
                                        <label htmlFor={id} className="block mb-1 text-[11px] text-slate-500">
                                            {REGISTRANT_LABELS[f]}
                                        </label>
                                        <TextInput
                                            id={id}
                                            value={r.value[f]}
                                            onChange={(v) => r.edit(f, f === "country" ? v.toUpperCase().slice(0, 2) : v)}
                                            onBlur={f === "phone" ? () => r.edit("phone", normalizePhone(r.value.phone)) : undefined}
                                            autoComplete={REGISTRANT_AUTOCOMPLETE[f]}
                                            placeholder={REGISTRANT_PLACEHOLDER[f]}
                                            invalid={!!err}
                                            className="w-full"
                                        />
                                        <FieldError message={err} />
                                    </div>
                                );
                            })}
                        </div>
                    </fieldset>
                </motion.div>
            )}
        </AnimatePresence>
    );
}

function PayArea({ checkout }: { checkout: CheckoutState }) {
    const reduced = useReducedMotion();
    if (checkout.planNeeded) return null;
    // The checklist covers what the buyer fills in; a missing permission or an unsellable domain is said in words.
    const listed = checkout.canPay && checkout.blocker !== null && checkout.checklist.some((c) => !c.done);
    return (
        <div className="flex flex-col items-stretch gap-2.5">
            <AnimatePresence initial={false}>
                {listed && (
                    <motion.div
                        key="checklist"
                        initial={reduced ? { opacity: 0 } : { opacity: 0, height: 0 }}
                        animate={{ opacity: 1, height: "auto" }}
                        exit={reduced ? { opacity: 0 } : { opacity: 0, height: 0 }}
                        transition={{ duration: 0.24, ease: EASE }}
                        className="overflow-hidden"
                    >
                        <motion.div animate={checkout.nudge && !reduced ? { x: [0, -4, 4, -2, 0] } : undefined} key={checkout.nudge} transition={{ duration: 0.3 }}>
                            <Checklist steps={checkout.checklist} />
                        </motion.div>
                    </motion.div>
                )}
            </AnimatePresence>
            <PayButton checkout={checkout} />
            <AnimatePresence initial={false} mode="wait">
                {checkout.blocker && !listed ? (
                    <motion.p
                        key={checkout.blocker}
                        role="status"
                        initial={{ opacity: 0, y: 3 }}
                        animate={checkout.nudge && !reduced ? { opacity: 1, y: 0, x: [0, -4, 4, -2, 0] } : { opacity: 1, y: 0 }}
                        exit={{ opacity: 0 }}
                        transition={{ duration: 0.3 }}
                        className="text-center text-[11.5px] text-slate-500 leading-snug"
                    >
                        {checkout.blocker}
                    </motion.p>
                ) : !checkout.blocker ? (
                    <motion.p key="fine" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="text-center text-[11px] text-slate-400">
                        You may be asked to confirm it is you before paying.
                    </motion.p>
                ) : null}
            </AnimatePresence>
        </div>
    );
}

/** What the order still needs, each ticking off as it is done. */
function Checklist({ steps }: { steps: PayStep[] }) {
    const reduced = useReducedMotion();
    return (
        <ul aria-label="Before you can pay" className="rounded-lg border border-slate-200 bg-slate-50/60 px-3 py-2 space-y-1">
            <AnimatePresence initial={false}>
                {steps.map((s) => (
                    <motion.li
                        key={s.key}
                        layout={reduced ? false : "position"}
                        initial={reduced ? { opacity: 0 } : { opacity: 0, height: 0 }}
                        animate={{ opacity: 1, height: "auto" }}
                        exit={reduced ? { opacity: 0 } : { opacity: 0, height: 0 }}
                        transition={{ duration: 0.2, ease: EASE }}
                        className="overflow-hidden"
                    >
                        <span className="flex items-center gap-2 py-0.5 text-[12px]">
                            <Tick done={s.done} />
                            <span className={cn("transition-colors duration-300", s.done ? "text-slate-400 line-through decoration-slate-300" : "text-slate-700")}>{s.label}</span>
                            <span className="sr-only">{s.done ? "done" : "to do"}</span>
                        </span>
                    </motion.li>
                ))}
            </AnimatePresence>
        </ul>
    );
}

function Tick({ done }: { done: boolean }) {
    const reduced = useReducedMotion();
    return (
        <motion.span
            aria-hidden="true"
            initial={false}
            animate={done && !reduced ? { scale: [0.7, 1.15, 1] } : { scale: 1 }}
            transition={{ duration: 0.32, ease: EASE }}
            className={cn("size-3.5 rounded-full border inline-flex items-center justify-center shrink-0 transition-colors duration-200", done ? "bg-emerald-500 border-emerald-500 text-white" : "border-slate-300 bg-white")}
        >
            {done && (
                <svg viewBox="0 0 16 16" className="w-2.5 h-2.5" fill="none">
                    <motion.path
                        d="M3.5 8.5l3 3 6-7"
                        stroke="currentColor"
                        strokeWidth="2.4"
                        strokeLinecap="round"
                        strokeLinejoin="round"
                        initial={reduced ? false : { pathLength: 0 }}
                        animate={{ pathLength: 1 }}
                        transition={{ duration: 0.28, ease: "easeOut" }}
                    />
                </svg>
            )}
        </motion.span>
    );
}

function busyLabel(busy: Busy, metered: boolean): string | null {
    switch (busy) {
        case "pricing":
            return "Checking the total…";
        case "card":
            return "Saving your card…";
        case "paying":
            return metered ? "Paying…" : "Placing order…";
        case "redirecting":
            return "Opening card setup…";
        default:
            return null;
    }
}

/** The one primary action: solid and high contrast, the amount on it; calm grey with the reasons above it while blocked. */
function PayButton({ checkout, compact = false }: { checkout: CheckoutState; compact?: boolean }) {
    const reduced = useReducedMotion();
    const busy = checkout.busy !== null;
    const blocked = !!checkout.blocker;
    const live = !reduced && !busy && !blocked;
    return (
        <motion.button
            type="button"
            onClick={checkout.pay}
            aria-disabled={blocked || busy || undefined}
            whileHover={live ? "hover" : undefined}
            whileTap={live ? { scale: 0.97 } : undefined}
            transition={PRESS_SPRING}
            className={cn(
                "group relative overflow-hidden rounded-xl font-semibold inline-flex items-center justify-center gap-2 outline-none focus-visible:ring-2 focus-visible:ring-sky-300 focus-visible:ring-offset-2 transition-[background-color,color,box-shadow,border-color] duration-200",
                compact ? "h-10 px-4 text-[13px] shrink-0" : "h-12 px-5 text-[14px] w-full",
                blocked
                    ? "bg-slate-100 text-slate-400 border border-slate-200 cursor-not-allowed"
                    : "bg-gradient-to-b from-slate-800 to-slate-950 text-white border border-slate-950 shadow-[inset_0_1px_0_rgba(255,255,255,0.14),0_1px_2px_rgba(15,23,42,0.25),0_10px_24px_-12px_rgba(15,23,42,0.6)] hover:from-slate-700 hover:to-slate-900",
                busy && "cursor-wait",
            )}
        >
            {busy && (
                <span aria-hidden="true" className="absolute inset-0 overflow-hidden">
                    <span className="absolute inset-y-0 -left-1/2 w-1/2 progress-sweep bg-gradient-to-r from-transparent via-white/25 to-transparent" />
                </span>
            )}
            {busy ? <Loader2Icon className="relative w-4 h-4 animate-spin" /> : <LockIcon className={cn("relative w-3.5 h-3.5", blocked ? "text-slate-400" : "text-slate-300")} />}
            <span className="relative tabular-nums tracking-[-0.01em]">{busyLabel(checkout.busy, checkout.metered) ?? checkout.label}</span>
            {!busy && !blocked && (
                <motion.span aria-hidden="true" variants={{ hover: { x: 3 } }} transition={PRESS_SPRING} className="relative inline-flex">
                    <ArrowRightIcon className="w-4 h-4 text-slate-300" />
                </motion.span>
            )}
        </motion.button>
    );
}
