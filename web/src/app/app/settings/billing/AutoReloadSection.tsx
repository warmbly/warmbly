// Automatic reload: the switch saves at once; stepper edits save together after
// a short pause. Both are optimistic and roll back on error. The three amounts
// bound each other so the server's 422s cannot happen.

import React from "react";
import { useQueryClient } from "@tanstack/react-query";
import { CreditCardIcon, Loader2Icon, RefreshCwIcon } from "lucide-react";
import { Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import UsdStepper from "@/components/app/billing/UsdStepper";
import { RELOAD_FAILURE_COPY, fmtClock, fmtDay } from "@/components/app/billing/balanceModel";
import { updateAutoReload } from "@/lib/api/client/app/subscription/balance";
import type { AppError } from "@/lib/api/client/normalizeError";
import {
    RESUME_AUTO_RELOAD_KEY,
    balanceQuery,
    useAddPaymentMethod,
    useUpdateAutoReload,
} from "@/lib/api/hooks/app/subscription/useBalance";
import type { AutoReload, AutoReloadPatch, Balance } from "@/lib/api/models/app/subscription/Credits";
import buildError from "@/lib/helper/buildError";
import { MAX_TOP_UP_USD, MIN_TOP_UP_USD, fmtUsd, fmtUsdTrim, roundCents } from "@/lib/money";
import toast from "react-hot-toast/headless";
import { cn } from "@/lib/utils";
import { Row, Section } from "../_components/SectionShell";

const SAVE_DELAY = 600;
const LIMIT_MAX = 50_000;
const THRESHOLD_MAX = 1_000;

type Draft = Partial<Omit<AutoReloadPatch, "enabled">>;

export default function AutoReloadSection({ balance }: { balance: Balance }) {
    const qc = useQueryClient();
    const save = useUpdateAutoReload();
    const addCard = useAddPaymentMethod();
    const [needsCard, setNeedsCard] = React.useState(false);
    const [draft, setDraft] = React.useState<Draft>({});
    const pending = React.useRef<Draft>({});
    const timer = React.useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
    const latest = React.useRef<AutoReload | undefined>(balance.auto_reload);
    latest.current = balance.auto_reload;

    // Leaving the page within the pause still saves the last edit.
    React.useEffect(
        () => () => {
            clearTimeout(timer.current);
            const p = pending.current;
            const ar = latest.current;
            if (!ar || Object.keys(p).length === 0) return;
            pending.current = {};
            void updateAutoReload(toPatch(ar, p))
                .then((next) => qc.setQueryData(balanceQuery.queryKey, next))
                .catch(() => qc.invalidateQueries({ queryKey: balanceQuery.queryKey }));
        },
        [qc],
    );

    const ar = balance.auto_reload;
    if (!ar) return null;
    const v: AutoReload = { ...ar, ...draft };
    const pm = balance.payment_method ?? null;

    const send = (patch: AutoReloadPatch) =>
        save.mutate(patch, {
            onError: (e) => {
                const err = e as unknown as AppError;
                if (err?.code === "payment_method_required") setNeedsCard(true);
                else toast.error(buildError(err));
            },
        });

    const flush = (enabled?: boolean) => {
        clearTimeout(timer.current);
        const p = pending.current;
        pending.current = {};
        setDraft({});
        if (enabled === undefined && Object.keys(p).length === 0) return;
        send({ ...toPatch(ar, p), ...(enabled === undefined ? {} : { enabled }) });
    };
    const edit = (patch: Draft) => {
        pending.current = { ...pending.current, ...patch };
        setDraft(pending.current);
        clearTimeout(timer.current);
        timer.current = setTimeout(() => flush(), SAVE_DELAY);
    };
    const goAddCard = (resume: boolean) => {
        if (resume) {
            try {
                sessionStorage.setItem(RESUME_AUTO_RELOAD_KEY, JSON.stringify({ ...toPatch(ar, pending.current), enabled: true }));
            } catch {
                /* private mode: turned on again by hand after the card is saved */
            }
        }
        addCard.mutate();
    };
    const toggle = (enabled: boolean) => {
        if (enabled && !pm) {
            goAddCard(true);
            return;
        }
        if (!enabled) setNeedsCard(false);
        flush(enabled);
    };

    // One reload must lift the balance back above the threshold and fit the monthly limit.
    const maxThreshold = Math.max(0.01, Math.min(THRESHOLD_MAX, roundCents(v.amount_usd - 0.01)));
    const minAmount = Math.min(MAX_TOP_UP_USD, Math.max(MIN_TOP_UP_USD, Math.floor(v.threshold_usd) + 1));
    const maxAmount = Math.max(minAmount, Math.min(MAX_TOP_UP_USD, v.monthly_limit_usd));
    const minLimit = Math.max(MIN_TOP_UP_USD, v.amount_usd);
    const failure = !v.enabled && ar.failure_code ? ar.failure_code : null;
    const retryAt = v.enabled && ar.retry_at ? new Date(ar.retry_at) : null;
    const last = ar.last_reload;

    return (
        <div id="auto-reload" className="scroll-mt-14">
            <Section
                eyebrow="Automatic reload"
                description={
                    v.enabled
                        ? `Adds ${fmtUsdTrim(v.amount_usd)} when the balance falls below ${fmtUsd(v.threshold_usd)}, up to ${fmtUsdTrim(v.monthly_limit_usd)} a month.`
                        : "Add money automatically when the balance runs low, so AI never pauses."
                }
                actions={
                    <div className="flex items-center gap-2 text-[12px] text-slate-500">
                        {addCard.isPending ? <Loader2Icon className="w-3 h-3 animate-spin" /> : v.enabled ? "On" : "Off"}
                        <Toggle value={v.enabled} onChange={toggle} ariaLabel="Automatic reload" disabled={addCard.isPending} />
                    </div>
                }
            >
                {failure && (
                    <Callout
                        tone="red"
                        title={`Automatic reload turned off${ar.failed_at ? ` ${fmtDay(ar.failed_at)}` : ""}`}
                        body={RELOAD_FAILURE_COPY[failure] ?? RELOAD_FAILURE_COPY.payment_failed}
                        action={
                            failure !== "unavailable" ? (
                                <CalloutButton onClick={() => goAddCard(true)} pending={addCard.isPending}>
                                    {pm ? "Update card" : "Add a card"}
                                </CalloutButton>
                            ) : undefined
                        }
                    />
                )}
                {retryAt && (
                    <Callout
                        tone="amber"
                        title="The bank declined the last reload for now."
                        body={
                            retryAt.toDateString() === new Date().toDateString()
                                ? `It's tried again at ${fmtClock(retryAt)}. Automatic reload stays on; update the card if it has changed.`
                                : `It's tried again ${fmtDay(retryAt)} at ${fmtClock(retryAt)}. Automatic reload stays on; update the card if it has changed.`
                        }
                        action={
                            <CalloutButton onClick={() => goAddCard(false)} pending={addCard.isPending}>
                                Update card
                            </CalloutButton>
                        }
                    />
                )}
                {needsCard && !v.enabled && !failure && (
                    <Callout
                        tone="amber"
                        title="Add a card to turn on automatic reload"
                        body="Stripe keeps the card. Each reload is charged to it with an invoice, up to your monthly limit."
                        action={
                            <CalloutButton primary onClick={() => goAddCard(true)} pending={addCard.isPending}>
                                Add a card
                            </CalloutButton>
                        }
                    />
                )}

                <Row label="Reload below" description="When the balance falls below this, add money.">
                    <UsdStepper
                        value={v.threshold_usd}
                        onCommit={(threshold_usd) => edit({ threshold_usd })}
                        min={0.01}
                        max={maxThreshold}
                        step={1}
                        ariaLabel="Reload below"
                        className="w-[132px]"
                    />
                </Row>
                <Row label="Reload amount" description="Added each time, dollar for dollar.">
                    <UsdStepper
                        value={v.amount_usd}
                        onCommit={(amount_usd) => edit({ amount_usd })}
                        min={minAmount}
                        max={maxAmount}
                        step={5}
                        ariaLabel="Reload amount"
                        className="w-[132px]"
                    />
                </Row>
                <Row label="Monthly limit" description="Reloads stop for the month once they have added this much.">
                    <UsdStepper
                        value={v.monthly_limit_usd}
                        onCommit={(monthly_limit_usd) => edit({ monthly_limit_usd })}
                        min={minLimit}
                        max={LIMIT_MAX}
                        step={25}
                        ariaLabel="Monthly limit"
                        className="w-[132px]"
                    />
                </Row>

                <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 pt-1 text-[11.5px]">
                    <span className="flex min-w-0 items-center gap-2 text-slate-500">
                        <RefreshCwIcon className="w-3 h-3 shrink-0 text-slate-400" aria-hidden />
                        <span className="min-w-0">
                            {last
                                ? `Last reload ${fmtDay(last.at)}: ${fmtUsd(last.amount_usd)}`
                                : v.enabled
                                  ? "No reloads yet"
                                  : "While it's off, the billing team gets a notice when the balance runs low."}
                        </span>
                    </span>
                    {v.enabled && (
                        <span className="shrink-0 font-mono tabular-nums text-slate-400">
                            {fmtUsd(ar.reloaded_this_month_usd)} of {fmtUsdTrim(v.monthly_limit_usd)} this month
                        </span>
                    )}
                </div>
            </Section>
        </div>
    );
}

function toPatch(ar: AutoReload, p: Draft): AutoReloadPatch {
    return {
        enabled: ar.enabled,
        threshold_usd: p.threshold_usd ?? ar.threshold_usd,
        amount_usd: p.amount_usd ?? ar.amount_usd,
        monthly_limit_usd: p.monthly_limit_usd ?? ar.monthly_limit_usd,
    };
}

function Callout({
    tone,
    title,
    body,
    action,
}: {
    tone: "red" | "amber";
    title: string;
    body: string;
    action?: React.ReactNode;
}) {
    return (
        <div
            role="status"
            className={cn(
                "flex flex-wrap sm:flex-nowrap items-start gap-3 rounded-md border px-3 py-2.5",
                tone === "red" ? "bg-red-50 border-red-200" : "bg-amber-50 border-amber-200",
            )}
        >
            <CreditCardIcon className={cn("w-3.5 h-3.5 mt-0.5 shrink-0", tone === "red" ? "text-red-600" : "text-amber-600")} />
            <div className="min-w-0 flex-1">
                <div className={cn("text-[12.5px] font-medium", tone === "red" ? "text-red-900" : "text-amber-900")}>{title}</div>
                <div className="mt-0.5 text-[11.5px] leading-relaxed text-slate-600">{body}</div>
            </div>
            {action && <div className="shrink-0">{action}</div>}
        </div>
    );
}

function CalloutButton({
    onClick,
    pending,
    primary,
    children,
}: {
    onClick: () => void;
    pending: boolean;
    primary?: boolean;
    children: React.ReactNode;
}) {
    return (
        <button
            type="button"
            onClick={onClick}
            disabled={pending}
            className={cn(
                "h-7 px-2.5 rounded-md text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60",
                primary
                    ? "bg-sky-600 hover:bg-sky-700 text-white"
                    : "border border-slate-200 bg-white hover:border-slate-300 text-slate-700 hover:text-slate-900",
            )}
        >
            {pending && <Loader2Icon className="w-3 h-3 animate-spin" />}
            {children}
        </button>
    );
}
