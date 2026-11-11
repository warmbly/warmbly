// UpgradeDialog — the plan chooser every plan lock and "Change plan" opens.
//
// A plain header naming what the user is choosing, the monthly/annual switch,
// the four plans as cards with one recommended, and one-click checkout through
// useUpgradeFlow. Non-owners get the same comparison with an "ask the owner"
// path instead of a dead CTA. Mounted once by UpgradeDialogProvider; open it
// with useUpgradeDialog().open({ feature, minPlan }).

import React from "react";
import { createPortal } from "react-dom";
import { Link } from "@tanstack/react-router";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import toast from "react-hot-toast/headless";
import {
    ArrowRightIcon,
    ArrowUpRightIcon,
    BuildingIcon,
    CheckIcon,
    Loader2Icon,
    LockIcon,
    ShieldCheckIcon,
    TicketIcon,
    XIcon,
} from "lucide-react";
import useFeatureAccess from "@/hooks/useFeatureAccess";
import useUpgradeFlow from "@/hooks/useUpgradeFlow";
import type { UpgradeRequest } from "@/hooks/context/upgrade";
import useValidateDiscountCode from "@/lib/api/hooks/app/subscription/useValidateDiscountCode";
import type DiscountPreview from "@/lib/api/models/app/subscription/DiscountPreview";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import useBrand from "@/hooks/useBrand";
import { PAID_PLANS, getPlan, isAtLeast, planOrder, type PlanID } from "@/lib/plans";
import { describeDiscount, type BillingInterval } from "@/lib/pricing";
import { TextInput } from "@/components/ui/field";
import { currentPortal } from "@/lib/portal";
import BillingIntervalToggle from "@/components/app/billing/BillingIntervalToggle";
import PlanCard from "@/components/app/billing/PlanCard";
import EnterpriseInquiryDialog from "@/components/app/billing/EnterpriseInquiryDialog";

const EASE = [0.22, 1, 0.36, 1] as const;

export default function UpgradeDialog({
    open,
    request,
    onClose,
}: {
    open: boolean;
    request: UpgradeRequest;
    onClose: () => void;
}) {
    const access = useFeatureAccess();
    const brand = useBrand();
    const flow = useUpgradeFlow();
    const validateCode = useValidateDiscountCode();
    const reduced = useReducedMotion();

    const [billingInterval, setBillingInterval] = React.useState<BillingInterval>("annual");
    const [promoOpen, setPromoOpen] = React.useState(false);
    const [codeInput, setCodeInput] = React.useState("");
    const [applied, setApplied] = React.useState<DiscountPreview | null>(null);
    const [salesOpen, setSalesOpen] = React.useState(false);

    const minPlan = getPlan(request.minPlan ?? "starter");
    const current = getPlan(access.plan).id;
    const busy = flow.pending !== null || flow.portalPending;

    // The card to spotlight: the cheapest plan that unlocks the feature and is
    // not below what the workspace already pays for.
    const recommended =
        PAID_PLANS.find((id) => isAtLeast(id, minPlan.id) && planOrder(id) > planOrder(current)) ??
        PAID_PLANS.find((id) => isAtLeast(id, minPlan.id)) ??
        "starter";

    // The card is marked data-floating so a dialog underneath ignores Escape
    // while this one is up. Escape here closes only this layer: any other
    // floating panel or the confirm owns it.
    const cardRef = React.useRef<HTMLDivElement>(null);

    // The card declares aria-modal, so honour it: move focus in on open, keep
    // Tab inside while it is up, and hand focus back to the trigger on close.
    // Without this a keyboard or screen-reader user tabs through the page
    // behind the backdrop and never reaches the plan buttons.
    React.useEffect(() => {
        if (!open) return;
        const previous = document.activeElement as HTMLElement | null;
        cardRef.current?.focus();

        const onTab = (e: KeyboardEvent) => {
            if (e.key !== "Tab") return;
            const card = cardRef.current;
            if (!card) return;
            // A nested layer (the enterprise inquiry, the confirm) owns Tab
            // while it is open.
            if (document.querySelector("[data-nested-modal], [role='alertdialog']")) return;
            const focusable = card.querySelectorAll<HTMLElement>(
                'a[href], button:not([disabled]), input:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
            );
            if (focusable.length === 0) return;
            const first = focusable[0];
            const last = focusable[focusable.length - 1];
            const active = document.activeElement;
            if (e.shiftKey && (active === first || active === card)) {
                e.preventDefault();
                last.focus();
            } else if (!e.shiftKey && active === last) {
                e.preventDefault();
                first.focus();
            }
        };
        document.addEventListener("keydown", onTab);
        return () => {
            document.removeEventListener("keydown", onTab);
            previous?.focus?.();
        };
    }, [open]);
    React.useEffect(() => {
        if (!open) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key !== "Escape") return;
            const others = Array.from(document.querySelectorAll("[data-floating]")).some(
                (el) => el !== cardRef.current,
            );
            if (others || document.querySelector("[data-nested-modal], [role='alertdialog']")) return;
            if (busy) return;
            onClose();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [open, busy, onClose]);

    // Lock the page behind the dialog while it is open.
    React.useEffect(() => {
        if (!open) return;
        const prev = document.body.style.overflow;
        document.body.style.overflow = "hidden";
        return () => {
            document.body.style.overflow = prev;
        };
    }, [open]);

    async function applyCode() {
        const code = codeInput.trim();
        if (!code) return;
        try {
            const res = await validateCode.mutateAsync({ code });
            if (res.valid) {
                setApplied(res);
                toast.success("Promo code applied");
            } else {
                setApplied(null);
                toast.error(res.reason || "That code can't be applied");
            }
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    }

    async function choose(id: PlanID) {
        const outcome = await flow.upgrade(id, {
            interval: billingInterval,
            discountCode: applied?.valid ? applied.code : undefined,
            returnTo: window.location.pathname + window.location.search,
        });
        if (outcome === "changed") onClose();
        if (outcome === "contact") setSalesOpen(true);
    }

    const requestClose = () => {
        if (!busy) onClose();
    };

    const titleId = "upgrade-dialog-title";

    return createPortal(
        <AnimatePresence>
            {open && (
                <motion.div
                    key="upgrade-backdrop"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.2 }}
                    onMouseDown={(e) => {
                        if (e.target === e.currentTarget) requestClose();
                    }}
                    className="fixed inset-0 z-[150] flex items-center justify-center bg-slate-900/40 backdrop-blur-[2px] p-3 sm:p-6"
                >
                    <motion.div
                        key="upgrade-card"
                        role="dialog"
                        aria-modal="true"
                        aria-labelledby={titleId}
                        data-floating
                        ref={cardRef}
                        tabIndex={-1}
                        initial={reduced ? { opacity: 0 } : { opacity: 0, y: 24, scale: 0.97 }}
                        animate={{ opacity: 1, y: 0, scale: 1 }}
                        exit={reduced ? { opacity: 0 } : { opacity: 0, y: 16, scale: 0.98 }}
                        transition={{ duration: 0.28, ease: EASE }}
                        onMouseDown={(e) => e.stopPropagation()}
                        className="relative w-full max-w-[1080px] max-h-[calc(100dvh-1.5rem)] sm:max-h-[calc(100dvh-3rem)] overflow-y-auto rounded-xl bg-white border border-slate-200 shadow-[0_1px_2px_rgba(15,23,42,0.06),0_32px_64px_-24px_rgba(15,23,42,0.35)]"
                    >
                        <Header
                            request={request}
                            planLabel={minPlan.label}
                            titleId={titleId}
                            onClose={requestClose}
                            busy={busy}
                            interval={billingInterval}
                            onInterval={setBillingInterval}
                        />

                        {/* Plan cards */}
                        <div className="px-5 md:px-8 pt-6 pb-5 grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-4 gap-3 items-stretch">
                            {PAID_PLANS.map((id, i) => (
                                <PlanCard
                                    key={id}
                                    id={id}
                                    index={i}
                                    // A chooser opened without a locked feature has nothing to judge the cards against.
                                    feature={request.title ? undefined : request.feature}
                                    minPlan={request.title ? undefined : minPlan.id}
                                    current={current}
                                    recommended={id === recommended}
                                    interval={billingInterval}
                                    discount={applied}
                                    canAct={access.isOwner}
                                    pending={flow.pending === id}
                                    busy={busy}
                                    onChoose={() => choose(id)}
                                />
                            ))}
                        </div>

                        {/* Promo code, and where agency plans are */}
                        <div className="px-5 md:px-8 pb-5 flex flex-wrap items-center justify-between gap-x-6 gap-y-2">
                            {!promoOpen && !applied ? (
                                <button
                                    type="button"
                                    onClick={() => setPromoOpen(true)}
                                    className="inline-flex items-center gap-1.5 text-[12px] text-slate-500 hover:text-slate-900 transition-colors"
                                >
                                    <TicketIcon className="w-3 h-3" />
                                    Have a promo code?
                                </button>
                            ) : (
                                <motion.div
                                    initial={reduced ? false : { opacity: 0, y: -4 }}
                                    animate={{ opacity: 1, y: 0 }}
                                    transition={{ duration: 0.18, ease: EASE }}
                                    className="flex flex-wrap items-center gap-2"
                                >
                                    <TextInput
                                        value={codeInput}
                                        onChange={(v) => setCodeInput(v.toUpperCase())}
                                        placeholder="WELCOME10"
                                        disabled={!!applied}
                                        autoFocus={!applied}
                                        onKeyDown={(e) => {
                                            if (e.key === "Enter") applyCode();
                                        }}
                                        className="w-[180px] font-mono uppercase"
                                    />
                                    {applied ? (
                                        <>
                                            <span className="text-[11.5px] text-emerald-700 bg-emerald-50 border border-emerald-100 rounded-md px-2 h-7 inline-flex items-center gap-1.5">
                                                <CheckIcon className="w-3 h-3 shrink-0" />
                                                <span className="font-mono font-medium">{applied.code}</span>
                                                <span>· {describeDiscount(applied)}</span>
                                            </span>
                                            <button
                                                type="button"
                                                onClick={() => {
                                                    setApplied(null);
                                                    setCodeInput("");
                                                }}
                                                className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 transition-colors inline-flex items-center gap-1"
                                            >
                                                <XIcon className="w-3 h-3" />
                                                Clear
                                            </button>
                                        </>
                                    ) : (
                                        <button
                                            type="button"
                                            onClick={applyCode}
                                            disabled={validateCode.isPending || !codeInput.trim()}
                                            className="h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                                        >
                                            {validateCode.isPending ? (
                                                <Loader2Icon className="w-3 h-3 animate-spin" />
                                            ) : (
                                                <TicketIcon className="w-3 h-3" />
                                            )}
                                            Apply
                                        </button>
                                    )}
                                </motion.div>
                            )}
                            {/* Agency plans belong to an agency, so they are chosen there; a portal domain is one agency's own. */}
                            {!currentPortal().portal && (
                                <Link
                                    to="/app/agency"
                                    onClick={onClose}
                                    className="inline-flex items-center gap-1.5 text-[12px] text-slate-500 hover:text-slate-900 transition-colors"
                                >
                                    <BuildingIcon className="w-3 h-3" />
                                    Outreach for clients? See agency plans
                                    <ArrowRightIcon className="w-3 h-3" />
                                </Link>
                            )}
                        </div>

                        {/* Footer */}
                        <div className="px-5 md:px-8 py-3.5 border-t border-slate-200 flex flex-col md:flex-row md:items-center gap-x-6 gap-y-2 text-[12px] text-slate-500">
                            {access.isOwner ? (
                                <p className="inline-flex items-center gap-1.5">
                                    <ShieldCheckIcon className="w-3.5 h-3.5 text-slate-400 shrink-0" />
                                    Secure checkout by Stripe. Cancel any time, and plan changes are prorated.
                                </p>
                            ) : (
                                <p className="leading-relaxed">
                                    Only the workspace owner can change the plan. Ask them to upgrade to{" "}
                                    <span className="font-medium text-slate-900">{minPlan.label}</span>, or see who does
                                    what in{" "}
                                    <Link
                                        to="/app/settings/roles"
                                        onClick={onClose}
                                        className="font-medium text-slate-700 hover:text-slate-900 underline underline-offset-2"
                                    >
                                        Roles and access
                                    </Link>
                                    .
                                </p>
                            )}
                            <div className="md:ml-auto flex flex-wrap items-center gap-x-4 gap-y-1">
                                {access.isOwner && (
                                    <Link
                                        to="/app/settings/billing/{-$tab}"
                                        params={{ tab: "plans" }}
                                        onClick={onClose}
                                        className="inline-flex items-center gap-1 font-medium text-slate-600 hover:text-slate-900 transition-colors"
                                    >
                                        Billing settings
                                        <ArrowRightIcon className="w-3 h-3" />
                                    </Link>
                                )}
                                {/* The pricing page is the service's own; an agency's website has none. */}
                                {brand.website_url && !currentPortal().portal && (
                                    <a
                                        href={`${brand.website_url}/pricing`}
                                        target="_blank"
                                        rel="noreferrer"
                                        className="inline-flex items-center gap-1 font-medium text-slate-600 hover:text-slate-900 transition-colors"
                                    >
                                        Compare every feature
                                        <ArrowUpRightIcon className="w-3 h-3" />
                                    </a>
                                )}
                            </div>
                        </div>
                    </motion.div>
                </motion.div>
            )}
            {salesOpen && (
                <EnterpriseInquiryDialog
                    key="enterprise-inquiry"
                    open={salesOpen}
                    onClose={() => setSalesOpen(false)}
                />
            )}
        </AnimatePresence>,
        document.body,
    );
}

function Header({
    request,
    planLabel,
    titleId,
    onClose,
    busy,
    interval,
    onInterval,
}: {
    request: UpgradeRequest;
    planLabel: string;
    titleId: string;
    onClose: () => void;
    busy: boolean;
    interval: BillingInterval;
    onInterval: (i: BillingInterval) => void;
}) {
    const bullets = request.bullets?.slice(0, 4);
    return (
        <div className="px-5 md:px-8 pt-6 pb-5 border-b border-slate-200 flex flex-col gap-4 md:flex-row md:items-start">
            <div className="min-w-0 flex-1 pr-10 md:pr-0">
                {/* A locked feature says which plan opens it; a plain chooser needs no such line. */}
                {!request.title && (
                    <span className="inline-flex items-center gap-1.5 h-5 px-1.5 rounded-md bg-slate-100 text-[11px] font-medium text-slate-600">
                        <LockIcon className="w-3 h-3" />
                        {planLabel} plan and up
                    </span>
                )}
                <h2
                    id={titleId}
                    className={`text-[20px] md:text-[22px] font-semibold tracking-[-0.02em] leading-tight text-slate-900 ${request.title ? "" : "mt-2.5"}`}
                >
                    {request.title ?? `${request.feature} is on the ${planLabel} plan`}
                </h2>
                <p className="mt-1.5 text-[13px] text-slate-500 leading-relaxed max-w-2xl">
                    {/* Only a locked feature promises that the feature turns on; a plain chooser compares plans. */}
                    {request.blurb ??
                        (request.title
                            ? "Compare every plan and pick monthly or annual billing."
                            : `Pick a plan and ${request.feature.toLowerCase()} turns on for your whole workspace the moment checkout completes.`)}
                </p>
                {bullets && bullets.length > 0 && (
                    <ul className="mt-3 flex flex-wrap gap-x-4 gap-y-1 text-[12px] text-slate-600">
                        {bullets.map((b) => (
                            <li key={b} className="inline-flex items-center gap-1.5">
                                <CheckIcon className="w-3 h-3 text-sky-600 shrink-0" />
                                {b}
                            </li>
                        ))}
                    </ul>
                )}
            </div>
            <div className="flex items-center gap-2 shrink-0 md:pr-10">
                <BillingIntervalToggle interval={interval} onChange={onInterval} size="md" />
            </div>
            <button
                type="button"
                onClick={onClose}
                disabled={busy}
                aria-label="Close"
                className="absolute top-4 right-4 size-8 rounded-md text-slate-400 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors disabled:opacity-50"
            >
                <XIcon className="w-4 h-4" />
            </button>
        </div>
    );
}
