// AI usage & spend controls for the billing page: spend per window against
// the configured limits, a dithered daily usage chart, breakdowns by feature
// and model, and the spend-control form. Limits are set per window through
// slider cells with quick presets; edits collect into a floating save bar so
// nothing writes until you commit.

import React from "react";
import toast from "react-hot-toast";
import { AnimatePresence, motion } from "framer-motion";
import { Loader2Icon, RotateCcwIcon } from "lucide-react";
import useCredits from "@/lib/api/hooks/app/subscription/useCredits";
import useCreditUsage from "@/lib/api/hooks/app/subscription/useCreditUsage";
import { useCreditSettings, useUpdateCreditSettings } from "@/lib/api/hooks/app/subscription/useCreditSettings";
import type { AppError } from "@/lib/api/client/normalizeError";
import type { AISpendSettings, CreditUsageBucket } from "@/lib/api/models/app/subscription/Credits";
import buildError from "@/lib/helper/buildError";
import { Label, NumberInput } from "@/components/ui/field";
import { SelectMenu } from "@/components/ui/select-menu";
import { AnimatedNumber, DitherBarChart, DitherMeter, DitherSlider, type DitherBarDatum, type DitherTone } from "@/components/ui/dither";
import { Section } from "../_components/SectionShell";

const REASON_LABELS: Record<string, string> = {
    writing_assistant: "Writing assistant",
    writing_edit: "Selection edits",
    reply_draft: "Reply drafts",
    agent_iteration: "Dashboard assistant",
    inbox_agent_draft: "Inbox agent",
    research_run: "Contact research",
    automation_ai: "Automation AI",
    campaign_ai: "Campaign switches",
};

type WindowKey = "day" | "week" | "month";

const WINDOW_PRESETS: Record<WindowKey, number[]> = {
    day: [50, 100, 250, 500],
    week: [250, 500, 1000, 2500],
    month: [500, 1000, 2500, 10000],
};

const WINDOW_SLIDER_MAX: Record<WindowKey, number> = {
    day: 1000,
    week: 5000,
    month: 20000,
};

const WINDOW_STEP: Record<WindowKey, number> = {
    day: 5,
    week: 25,
    month: 50,
};

interface SpendForm {
    daily: number | null;
    weekly: number | null;
    monthly: number | null;
    memberDaily: number | null;
    memberWeekly: number | null;
    memberMonthly: number | null;
    lowThreshold: number;
    autoEnabled: boolean;
    autoPack: string;
    autoThreshold: number;
    autoMax: number;
}

function toForm(s: AISpendSettings): SpendForm {
    return {
        daily: s.spend_limit_daily,
        weekly: s.spend_limit_weekly,
        monthly: s.spend_limit_monthly,
        memberDaily: s.member_limit_daily,
        memberWeekly: s.member_limit_weekly,
        memberMonthly: s.member_limit_monthly,
        lowThreshold: s.low_balance_threshold,
        autoEnabled: s.auto_topup_enabled,
        autoPack: s.auto_topup_pack,
        autoThreshold: s.auto_topup_threshold,
        autoMax: s.auto_topup_max_per_month,
    };
}

export default function AIUsageCard() {
    const usage = useCreditUsage(30);
    const settings = useCreditSettings();
    const credits = useCredits();
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
                    spend_limit_daily: form.daily,
                    spend_limit_weekly: form.weekly,
                    spend_limit_monthly: form.monthly,
                    member_limit_daily: form.memberDaily,
                    member_limit_weekly: form.memberWeekly,
                    member_limit_monthly: form.memberMonthly,
                    low_balance_threshold: form.lowThreshold,
                    auto_topup_enabled: form.autoEnabled,
                    auto_topup_pack: form.autoPack,
                    auto_topup_threshold: form.autoThreshold,
                    auto_topup_max_per_month: form.autoMax,
                }),
                {
                    loading: "Saving spend controls…",
                    success: "Spend controls saved",
                    error: (e: AppError) => buildError(e),
                },
            );
        } catch {
            /* surfaced via toast */
        }
    };

    const u = usage.data;
    const packs = credits.data?.packs ?? [];

    const chartData = React.useMemo<DitherBarDatum[]>(() => {
        if (!u) return [];
        const byDate = new Map(u.series.map((p) => [p.date, p]));
        const days: DitherBarDatum[] = [];
        const now = new Date();
        for (let i = 29; i >= 0; i--) {
            const d = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate() - i));
            const key = d.toISOString().slice(0, 10);
            const p = byDate.get(key);
            const label = d.toLocaleDateString("en-US", { month: "short", day: "numeric", timeZone: "UTC" });
            days.push({
                key,
                value: p?.credits ?? 0,
                hint: `${label} · ${(p?.credits ?? 0).toLocaleString()} credits · ${(p?.tokens ?? 0).toLocaleString()} tokens`,
            });
        }
        return days;
    }, [u]);

    return (
        <Section
            eyebrow="AI usage & spend controls"
            description="Credits are charged from what each AI call actually uses (tokens, per model). Watch the spend here, cap it per day, week, or month, and top up automatically before features pause."
        >
            {usage.isPending ? (
                <div className="h-24 rounded bg-slate-100 animate-pulse" />
            ) : u ? (
                <>
                    <div className="grid grid-cols-1 sm:grid-cols-3 gap-2">
                        <SpendStat label="Today" spent={u.spent_today} limit={u.limit_daily} />
                        <SpendStat label="This week" spent={u.spent_week} limit={u.limit_weekly} />
                        <SpendStat label="This month" spent={u.spent_month} limit={u.limit_monthly} />
                    </div>

                    <div>
                        <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 mb-2">Last 30 days</div>
                        <DitherBarChart data={chartData} height={72} />
                    </div>

                    <div className="grid grid-cols-1 md:grid-cols-2 gap-x-8 gap-y-4">
                        <Breakdown title="By feature" rows={u.by_reason} labelFor={(k) => REASON_LABELS[k] ?? k} />
                        <Breakdown title="By model" rows={u.by_model} labelFor={(k) => k || "unmetered"} />
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
                                spent={u?.spent_today}
                                spentLabel="Spent today"
                            />
                            <LimitCell
                                label="Per week"
                                windowKey="week"
                                value={form.weekly}
                                onChange={(v) => setForm({ ...form, weekly: v })}
                                spent={u?.spent_week}
                                spentLabel="Spent this week"
                            />
                            <LimitCell
                                label="Per month"
                                windowKey="month"
                                value={form.monthly}
                                onChange={(v) => setForm({ ...form, monthly: v })}
                                spent={u?.spent_month}
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
                        <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 mb-2">Low-credit reminder</div>
                        <div className="flex flex-wrap items-center gap-2 text-[12px] text-slate-600">
                            <span>Alert the billing team when the balance drops below</span>
                            <NumberInput
                                value={form.lowThreshold}
                                onChange={(n) => setForm({ ...form, lowThreshold: Math.max(0, Math.round(n)) })}
                                min={0}
                                max={1000000}
                                className="w-24"
                            />
                            <span>credits (at most once per day).</span>
                        </div>
                    </div>

                    <div>
                        <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 mb-2">Auto top-up</div>
                        <div className="flex items-center gap-2">
                            <button
                                type="button"
                                role="switch"
                                aria-checked={form.autoEnabled}
                                aria-label="Auto top-up"
                                onClick={() => setForm({ ...form, autoEnabled: !form.autoEnabled })}
                                className={`relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors ${
                                    form.autoEnabled ? "bg-sky-600" : "bg-slate-200"
                                }`}
                            >
                                <span
                                    className={`inline-block size-4 transform rounded-full bg-white shadow-sm transition-transform ${
                                        form.autoEnabled ? "translate-x-4" : "translate-x-0.5"
                                    }`}
                                />
                            </button>
                            <span className="text-[12px] text-slate-600">
                                Buy a pack automatically with the card on file when credits run low
                            </span>
                        </div>
                        <AnimatePresence initial={false}>
                            {form.autoEnabled && (
                                <motion.div
                                    initial={{ height: 0, opacity: 0 }}
                                    animate={{ height: "auto", opacity: 1 }}
                                    exit={{ height: 0, opacity: 0 }}
                                    transition={{ duration: 0.18, ease: "easeOut" }}
                                    className="overflow-hidden"
                                >
                                    <div className="mt-2 grid grid-cols-1 sm:grid-cols-3 gap-2">
                                        <div>
                                            <Label>Pack to buy</Label>
                                            <SelectMenu
                                                value={form.autoPack}
                                                onChange={(v) => setForm({ ...form, autoPack: v })}
                                                options={packs.map((p) => ({ value: p.key, label: `${p.credits.toLocaleString()} credits` }))}
                                                className="w-full"
                                                fullWidth
                                            />
                                        </div>
                                        <div>
                                            <Label>When balance is below</Label>
                                            <NumberInput
                                                value={form.autoThreshold}
                                                onChange={(n) => setForm({ ...form, autoThreshold: Math.max(0, Math.round(n)) })}
                                                min={0}
                                                max={1000000}
                                                className="w-full"
                                            />
                                        </div>
                                        <div>
                                            <Label>Max purchases / month</Label>
                                            <NumberInput
                                                value={form.autoMax}
                                                onChange={(n) => setForm({ ...form, autoMax: Math.max(0, Math.min(100, Math.round(n))) })}
                                                min={0}
                                                max={100}
                                                className="w-full"
                                            />
                                        </div>
                                    </div>
                                </motion.div>
                            )}
                        </AnimatePresence>
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
                                    <span className="text-[12px] text-slate-600">Unsaved spend controls</span>
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
                <AnimatedNumber value={spent} className="text-[16px] font-semibold text-slate-900 tabular-nums" />
                <span className="text-[11px] text-slate-400">{limit ? `/ ${limit.toLocaleString()} credits` : "credits"}</span>
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
    const total = Math.max(1, rows.reduce((sum, r) => sum + r.credits, 0));
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
                                    {r.credits.toLocaleString()} cr · {r.tokens.toLocaleString()} tok · {r.count.toLocaleString()}×
                                </span>
                            </div>
                            <DitherMeter frac={r.credits / total} height={4} className="mt-0.5" />
                        </div>
                    ))}
                </div>
            )}
        </div>
    );
}

function fmtCompact(n: number): string {
    return n >= 1000 ? `${n % 1000 === 0 ? n / 1000 : (n / 1000).toFixed(1)}k` : String(n);
}

// LimitCell is one spend-limit control: an on/off toggle, then a slider with
// quick presets and the exact number, plus live spent-vs-limit context when
// the current window's spend is known.
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
    const pct = on && spent !== undefined ? Math.min(100, Math.round((spent / Math.max(1, value)) * 100)) : null;
    return (
        <div className="rounded-md border border-slate-200 bg-white px-3 py-2">
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
            <AnimatePresence initial={false}>
                {on && (
                    <motion.div
                        initial={{ height: 0, opacity: 0 }}
                        animate={{ height: "auto", opacity: 1 }}
                        exit={{ height: 0, opacity: 0 }}
                        transition={{ duration: 0.18, ease: "easeOut" }}
                        className="overflow-hidden"
                    >
                        <div className="pt-2 space-y-2">
                            <DitherSlider
                                value={value}
                                min={1}
                                max={sliderMax}
                                step={WINDOW_STEP[windowKey]}
                                onChange={(v) => onChange(v)}
                                label={`${label} limit`}
                            />
                            <div className="flex items-center gap-1.5">
                                <NumberInput
                                    value={value}
                                    onChange={(n) => onChange(Math.max(1, Math.round(n)))}
                                    min={1}
                                    max={1000000}
                                    className="w-full"
                                />
                                <span className="text-[11px] text-slate-400">credits</span>
                            </div>
                            <div className="flex flex-wrap gap-1">
                                {presets.map((p) => (
                                    <button
                                        key={p}
                                        type="button"
                                        onClick={() => onChange(p)}
                                        className={`h-5 px-1.5 rounded text-[10.5px] font-medium tabular-nums border transition-colors ${
                                            value === p
                                                ? "bg-sky-50 text-sky-700 border-sky-200"
                                                : "border-slate-200 text-slate-500 hover:text-slate-700 hover:border-slate-300"
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
                                            {spent.toLocaleString()} / {(value ?? 0).toLocaleString()}
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
