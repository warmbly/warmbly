// AI spend and spend controls on the Balance tab: dollars spent per window
// against the limits, a 7/30/90 day graph, breakdowns by feature and model,
// and the limits form. Edits collect into a floating save bar.

import React from "react";
import useBrowseState from "@/hooks/useBrowseState";
import { aiUsageRangeSchema } from "@/lib/browse-other-lists";
import toast from "react-hot-toast/headless";
import { AnimatePresence, motion } from "framer-motion";
import { Loader2Icon, RotateCcwIcon } from "lucide-react";
import useCreditUsage from "@/lib/api/hooks/app/subscription/useCreditUsage";
import { useCreditSettings, useUpdateCreditSettings } from "@/lib/api/hooks/app/subscription/useCreditSettings";
import type { AppError } from "@/lib/api/client/normalizeError";
import type { AISpendSettings, CreditUsageBucket } from "@/lib/api/models/app/subscription/Credits";
import buildError from "@/lib/helper/buildError";
import UsdStepper from "@/components/app/billing/UsdStepper";
import { reasonLabel } from "@/components/app/billing/balanceModel";
import { fmtUsd, fmtUsdTrim, parseUsd } from "@/lib/money";
import { AnimatedNumber, DitherAreaChart, DitherMeter, DitherSlider, type DitherBarDatum, type DitherTone } from "@/components/ui/dither";
import { Section } from "../_components/SectionShell";

type WindowKey = "day" | "week" | "month";

const WINDOW_PRESETS: Record<WindowKey, number[]> = {
    day: [1, 5, 10, 25],
    week: [5, 25, 50, 100],
    month: [25, 100, 250, 1000],
};

const WINDOW_SLIDER_MAX: Record<WindowKey, number> = {
    day: 100,
    week: 500,
    month: 2000,
};

const WINDOW_STEP: Record<WindowKey, number> = {
    day: 1,
    week: 5,
    month: 25,
};

interface SpendForm {
    daily: number | null;
    weekly: number | null;
    monthly: number | null;
    memberDaily: number | null;
    memberWeekly: number | null;
    memberMonthly: number | null;
    lowThreshold: number;
}

function toForm(s: AISpendSettings): SpendForm {
    return {
        daily: s.spend_limit_daily_usd ?? null,
        weekly: s.spend_limit_weekly_usd ?? null,
        monthly: s.spend_limit_monthly_usd ?? null,
        memberDaily: s.member_limit_daily_usd ?? null,
        memberWeekly: s.member_limit_weekly_usd ?? null,
        memberMonthly: s.member_limit_monthly_usd ?? null,
        lowThreshold: s.low_balance_usd ?? 0,
    };
}

type RangeDays = 7 | 30 | 90;

export default function AIUsageCard() {
    const [range, setRange] = useBrowseState<RangeDays>("settings.billing.ai-usage.range", 30, aiUsageRangeSchema);
    const usage = useCreditUsage(range);
    const settings = useCreditSettings();
    const save = useUpdateCreditSettings();

    // Local form state, seeded from the server row once it loads.
    const [form, setForm] = React.useState<SpendForm | null>(null);
    const serverForm = React.useMemo(
        () => (settings.data ? toForm(settings.data) : null),
        [settings.data],
    );
    React.useEffect(() => {
        if (serverForm) setForm(serverForm);
    }, [serverForm]);

    const dirty =
        !!form && !!serverForm && JSON.stringify(form) !== JSON.stringify(serverForm);

    const submit = async () => {
        if (!form) return;
        try {
            await toast.promise(
                save.mutateAsync({
                    spend_limit_daily_usd: form.daily,
                    spend_limit_weekly_usd: form.weekly,
                    spend_limit_monthly_usd: form.monthly,
                    member_limit_daily_usd: form.memberDaily,
                    member_limit_weekly_usd: form.memberWeekly,
                    member_limit_monthly_usd: form.memberMonthly,
                    low_balance_usd: form.lowThreshold,
                }),
                {
                    loading: "Saving limits…",
                    success: "Limits saved",
                    error: (e: AppError) => buildError(e),
                },
            );
        } catch {
            /* surfaced via toast */
        }
    };

    const u = usage.data;

    const chartData = React.useMemo<DitherBarDatum[]>(() => {
        if (!u) return [];
        const byDate = new Map(u.series.map((p) => [p.date, p]));
        const days: DitherBarDatum[] = [];
        const now = new Date();
        for (let i = range - 1; i >= 0; i--) {
            const d = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate() - i));
            const key = d.toISOString().slice(0, 10);
            const p = byDate.get(key);
            const label = d.toLocaleDateString("en-US", { month: "short", day: "numeric", timeZone: "UTC" });
            days.push({
                key,
                value: p?.amount_usd ?? 0,
                hint: `${label} · ${fmtUsd(p?.amount_usd ?? 0)} · ${(p?.tokens ?? 0).toLocaleString()} tokens`,
            });
        }
        return days;
    }, [u, range]);

    return (
        <Section
            eyebrow="AI spend & limits"
            description="Each AI call is charged at what it actually uses (tokens, per model). Watch the spend here and cap it per day, week or month."
        >
            {usage.isPending ? (
                <div className="h-24 rounded bg-slate-100 animate-pulse" />
            ) : u ? (
                <>
                    <div className="grid grid-cols-1 sm:grid-cols-3 gap-2">
                        <SpendStat label="Today" spent={u.spent_today_usd} limit={u.limit_daily_usd} />
                        <SpendStat label="This week" spent={u.spent_week_usd} limit={u.limit_weekly_usd} />
                        <SpendStat label="This month" spent={u.spent_month_usd} limit={u.limit_monthly_usd} />
                    </div>

                    <div>
                        <div className="mb-2 flex items-center justify-between gap-2">
                            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400">
                                Last {range} days
                            </div>
                            <RangeToggle range={range} onChange={setRange} />
                        </div>
                        <DitherAreaChart data={chartData} height={96} />
                    </div>

                    <div className="grid grid-cols-1 md:grid-cols-2 gap-x-8 gap-y-4">
                        <Breakdown title="By feature" rows={u.by_reason} labelFor={reasonLabel} />
                        <Breakdown title="By model" rows={u.by_model} labelFor={(k) => k || "No AI model"} />
                    </div>
                </>
            ) : (
                <p className="text-[11.5px] text-slate-500">Usage is unavailable right now.</p>
            )}

            {form && (
                <div className="mt-2 border-t border-slate-200 pt-4 space-y-5">
                    <div>
                        <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 mb-2">Workspace limits</div>
                        <div className="grid grid-cols-1 sm:grid-cols-3 gap-2">
                            <LimitCell
                                label="Per day"
                                windowKey="day"
                                value={form.daily}
                                onChange={(v) => setForm({ ...form, daily: v })}
                                spent={u?.spent_today_usd}
                                spentLabel="Spent today"
                            />
                            <LimitCell
                                label="Per week"
                                windowKey="week"
                                value={form.weekly}
                                onChange={(v) => setForm({ ...form, weekly: v })}
                                spent={u?.spent_week_usd}
                                spentLabel="Spent this week"
                            />
                            <LimitCell
                                label="Per month"
                                windowKey="month"
                                value={form.monthly}
                                onChange={(v) => setForm({ ...form, monthly: v })}
                                spent={u?.spent_month_usd}
                                spentLabel="Spent this month"
                            />
                        </div>
                        <p className="mt-1.5 text-[11px] text-slate-400">
                            When a limit is reached, AI features pause until the window rolls over. Leave a limit off for no cap.
                        </p>
                    </div>

                    <div>
                        <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 mb-2">Per-member limits</div>
                        <div className="grid grid-cols-1 sm:grid-cols-3 gap-2">
                            <LimitCell
                                label="Per day"
                                windowKey="day"
                                value={form.memberDaily}
                                onChange={(v) => setForm({ ...form, memberDaily: v })}
                            />
                            <LimitCell
                                label="Per week"
                                windowKey="week"
                                value={form.memberWeekly}
                                onChange={(v) => setForm({ ...form, memberWeekly: v })}
                            />
                            <LimitCell
                                label="Per month"
                                windowKey="month"
                                value={form.memberMonthly}
                                onChange={(v) => setForm({ ...form, memberMonthly: v })}
                            />
                        </div>
                        <p className="mt-1.5 text-[11px] text-slate-400">
                            Caps what each teammate can spend on AI in each window. Scheduled work (automations, inbox agent) is workspace-level and not counted against anyone. Who can use AI at all is a role permission ("Use AI") under Team.
                        </p>
                    </div>

                    <div>
                        <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 mb-2">Low balance notice</div>
                        <div className="flex flex-wrap items-center gap-2 text-[12px] text-slate-600">
                            <span>Tell the billing team when the balance drops below</span>
                            <UsdStepper
                                value={form.lowThreshold}
                                onCommit={(n) => setForm({ ...form, lowThreshold: n })}
                                min={0}
                                max={1000}
                                step={1}
                                ariaLabel="Low balance notice"
                                className="w-28"
                            />
                        </div>
                        <p className="mt-1.5 text-[11px] text-slate-400">
                            Sent once until money is added, and not while automatic reload covers it.
                        </p>
                    </div>

                    <AnimatePresence>
                        {dirty && (
                            <motion.div
                                initial={{ opacity: 0, y: 10 }}
                                animate={{ opacity: 1, y: 0 }}
                                exit={{ opacity: 0, y: 10 }}
                                transition={{ duration: 0.16, ease: "easeOut" }}
                                className="sticky bottom-4 z-10 flex justify-center"
                            >
                                <div className="flex items-center gap-2 rounded-md border border-slate-200 bg-white px-3 py-2 shadow-lg shadow-slate-900/10">
                                    <span className="text-[12px] text-slate-600">Unsaved limits</span>
                                    <button
                                        type="button"
                                        onClick={() => serverForm && setForm(serverForm)}
                                        disabled={save.isPending}
                                        className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 transition-colors inline-flex items-center gap-1.5 disabled:opacity-60"
                                    >
                                        <RotateCcwIcon className="w-3 h-3" />
                                        Reset
                                    </button>
                                    <button
                                        type="button"
                                        onClick={submit}
                                        disabled={save.isPending}
                                        className="h-7 px-3 rounded-md bg-sky-600 text-[12px] font-medium text-white hover:bg-sky-700 inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                                    >
                                        {save.isPending && <Loader2Icon className="w-3 h-3 animate-spin" />}
                                        Save
                                    </button>
                                </div>
                            </motion.div>
                        )}
                    </AnimatePresence>
                </div>
            )}
        </Section>
    );
}

function meterTone(pct: number): DitherTone {
    return pct >= 90 ? "rose" : pct >= 70 ? "amber" : "sky";
}

// SpendStat is one window's spent-vs-limit chip with a dithered meter when a
// limit is configured.
function SpendStat({ label, spent, limit }: { label: string; spent: number; limit: number | null }) {
    const pct = limit && limit > 0 ? Math.min(100, Math.round((spent / limit) * 100)) : null;
    return (
        <div className="rounded-md border border-slate-200 bg-white px-3 py-2">
            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400">{label}</div>
            <div className="mt-0.5 flex items-baseline gap-1">
                <AnimatedNumber value={spent} format={fmtUsd} className="text-[16px] font-semibold text-slate-900 tabular-nums" />
                {limit ? <span className="text-[11px] text-slate-400">of {fmtUsdTrim(limit)}</span> : null}
            </div>
            {pct !== null && <DitherMeter frac={pct / 100} tone={meterTone(pct)} height={4} className="mt-1.5" />}
        </div>
    );
}

function Breakdown({
    title,
    rows,
    labelFor,
}: {
    title: string;
    rows: CreditUsageBucket[];
    labelFor: (key: string) => string;
}) {
    const total = Math.max(0.000001, rows.reduce((sum, r) => sum + r.amount_usd, 0));
    return (
        <div>
            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 mb-2">{title}</div>
            {rows.length === 0 ? (
                <p className="text-[11.5px] text-slate-400">No AI usage in this window yet.</p>
            ) : (
                <div className="space-y-1.5">
                    {rows.map((r) => (
                        <div key={r.key} className="text-[11.5px]">
                            <div className="flex items-baseline justify-between gap-2">
                                <span className="min-w-0 truncate text-slate-600">{labelFor(r.key)}</span>
                                <span className="shrink-0 tabular-nums text-slate-500">
                                    {fmtUsd(r.amount_usd)} · {r.tokens.toLocaleString()} tok · {r.count.toLocaleString()}×
                                </span>
                            </div>
                            <DitherMeter frac={r.amount_usd / total} height={4} className="mt-0.5" />
                        </div>
                    ))}
                </div>
            )}
        </div>
    );
}

function fmtCompact(n: number): string {
    return n >= 1000 ? `$${n % 1000 === 0 ? n / 1000 : (n / 1000).toFixed(1)}k` : fmtUsdTrim(n);
}

// RangeToggle switches the usage graph window (the backend serves 1-90 days).
function RangeToggle({ range, onChange }: { range: RangeDays; onChange: (r: RangeDays) => void }) {
    return (
        <div className="inline-flex items-center rounded-md border border-slate-200 bg-slate-50 p-0.5 text-[11px]">
            {([7, 30, 90] as RangeDays[]).map((r) => {
                const active = range === r;
                return (
                    <button
                        key={r}
                        type="button"
                        onClick={() => onChange(r)}
                        className={`h-5 px-2 rounded font-medium tabular-nums transition-colors ${
                            active ? "bg-white text-slate-900 shadow-sm" : "text-slate-500 hover:text-slate-700"
                        }`}
                    >
                        {r}d
                    </button>
                );
            })}
        </div>
    );
}

// InlineNumber renders a dollar value as editable text (no input chrome):
// click to type an exact amount, Enter/blur commits.
function InlineNumber({
    value,
    onCommit,
    className,
}: {
    value: number;
    onCommit: (n: number) => void;
    className?: string;
}) {
    const show = (n: number) => (Number.isInteger(n) ? String(n) : n.toFixed(2));
    const [text, setText] = React.useState(show(value));
    React.useEffect(() => {
        setText(Number.isInteger(value) ? String(value) : value.toFixed(2));
    }, [value]);
    const commit = () => {
        const n = Math.max(0.01, parseUsd(text) ?? value);
        setText(show(n));
        if (n !== value) onCommit(n);
    };
    return (
        <input
            type="text"
            inputMode="decimal"
            aria-label="Limit in US dollars"
            value={text}
            onChange={(e) => setText(e.target.value.replace(/[^\d.,]/g, ""))}
            onFocus={(e) => e.currentTarget.select()}
            onBlur={commit}
            onKeyDown={(e) => {
                if (e.key === "Enter") e.currentTarget.blur();
            }}
            style={{ width: `${Math.max(2, text.length)}ch` }}
            className={`border-0 border-b border-dashed border-slate-300 bg-transparent p-0 outline-none transition-colors hover:border-slate-400 focus:border-solid focus:border-sky-500 ${className ?? ""}`}
        />
    );
}

// LimitCell is one spend-limit control: an on/off toggle, the value as a big
// inline-editable number, a dithered slider with quiet presets, and live
// spent-vs-limit context when the current window's spend is known.
function LimitCell({
    label,
    windowKey,
    value,
    onChange,
    spent,
    spentLabel,
}: {
    label: string;
    windowKey: WindowKey;
    value: number | null;
    onChange: (v: number | null) => void;
    spent?: number;
    spentLabel?: string;
}) {
    const on = value !== null;
    const presets = WINDOW_PRESETS[windowKey];
    const sliderMax = Math.max(WINDOW_SLIDER_MAX[windowKey], value ?? 0);
    const pct = on && spent !== undefined ? Math.min(100, Math.round((spent / Math.max(0.01, value)) * 100)) : null;
    return (
        <div className="rounded-md border border-slate-200 bg-white px-3 py-2.5">
            <div className="flex items-center justify-between">
                <span className="text-[11.5px] font-medium text-slate-700">{label}</span>
                <button
                    type="button"
                    role="switch"
                    aria-checked={on}
                    aria-label={`${label} limit`}
                    onClick={() => onChange(on ? null : presets[1])}
                    className={`relative inline-flex h-[18px] w-8 shrink-0 items-center rounded-full transition-colors ${
                        on ? "bg-sky-600" : "bg-slate-200"
                    }`}
                >
                    <span
                        className={`inline-block h-3.5 w-3.5 transform rounded-full bg-white shadow-sm transition-transform ${
                            on ? "translate-x-[15px]" : "translate-x-[2px]"
                        }`}
                    />
                </button>
            </div>
            {!on && <div className="mt-1 text-[12px] text-slate-400">No cap</div>}
            <AnimatePresence initial={false}>
                {on && (
                    <motion.div
                        initial={{ height: 0, opacity: 0 }}
                        animate={{ height: "auto", opacity: 1 }}
                        exit={{ height: 0, opacity: 0 }}
                        transition={{ duration: 0.18, ease: "easeOut" }}
                        className="overflow-hidden"
                    >
                        <div className="pt-1.5 space-y-2">
                            <div className="flex items-baseline gap-0.5">
                                <span className="text-[17px] font-semibold text-slate-400">$</span>
                                <InlineNumber
                                    value={value}
                                    onCommit={(n) => onChange(n)}
                                    className="text-[17px] font-semibold text-slate-900 tabular-nums"
                                />
                            </div>
                            <DitherSlider
                                value={value}
                                min={1}
                                max={sliderMax}
                                step={WINDOW_STEP[windowKey]}
                                onChange={(v) => onChange(v)}
                                label={`${label} limit`}
                            />
                            <div className="flex items-center gap-2.5">
                                {presets.map((p) => (
                                    <button
                                        key={p}
                                        type="button"
                                        onClick={() => onChange(p)}
                                        className={`text-[10.5px] tabular-nums transition-colors ${
                                            value === p
                                                ? "font-semibold text-sky-700"
                                                : "text-slate-400 hover:text-sky-700"
                                        }`}
                                    >
                                        {fmtCompact(p)}
                                    </button>
                                ))}
                            </div>
                            {pct !== null && spent !== undefined && (
                                <div>
                                    <div className="flex items-baseline justify-between text-[10.5px] text-slate-400">
                                        <span>{spentLabel}</span>
                                        <span className="tabular-nums">
                                            {fmtUsd(spent)} / {fmtUsdTrim(value ?? 0)}
                                        </span>
                                    </div>
                                    <DitherMeter frac={pct / 100} tone={meterTone(pct)} height={3} className="mt-1" />
                                </div>
                            )}
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}
