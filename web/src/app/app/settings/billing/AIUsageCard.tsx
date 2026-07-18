// AI usage & spend controls for the billing page: spend per window against
// the configured limits, a daily usage chart, breakdowns by feature and model,
// and the spend-control form (day/week/month limits, low-balance alert
// threshold, auto top-up). Credits are charged from real usage (tokens), so
// this card is where that spend becomes visible and boundable.

import React from "react";
import toast from "react-hot-toast";
import { Loader2Icon } from "lucide-react";
import useCredits from "@/lib/api/hooks/app/subscription/useCredits";
import useCreditUsage from "@/lib/api/hooks/app/subscription/useCreditUsage";
import { useCreditSettings, useUpdateCreditSettings } from "@/lib/api/hooks/app/subscription/useCreditSettings";
import type { AppError } from "@/lib/api/client/normalizeError";
import type { CreditUsageBucket } from "@/lib/api/models/app/subscription/Credits";
import buildError from "@/lib/helper/buildError";
import { Label, NumberInput } from "@/components/ui/field";
import { SelectMenu } from "@/components/ui/select-menu";
import { Section } from "../_components/SectionShell";

const REASON_LABELS: Record<string, string> = {
    writing_assistant: "Writing assistant",
    reply_draft: "Reply drafts",
    agent_iteration: "Dashboard assistant",
    inbox_agent_draft: "Inbox agent",
    research_run: "Contact research",
    automation_ai: "Automation AI",
    campaign_ai: "Campaign switches",
};

export default function AIUsageCard() {
    const usage = useCreditUsage(30);
    const settings = useCreditSettings();
    const credits = useCredits();
    const save = useUpdateCreditSettings();

    // Local form state, seeded from the server row once it loads.
    const [form, setForm] = React.useState<{
        daily: number | null;
        weekly: number | null;
        monthly: number | null;
        lowThreshold: number;
        autoEnabled: boolean;
        autoPack: string;
        autoThreshold: number;
        autoMax: number;
    } | null>(null);
    React.useEffect(() => {
        const s = settings.data;
        if (!s) return;
        setForm({
            daily: s.spend_limit_daily,
            weekly: s.spend_limit_weekly,
            monthly: s.spend_limit_monthly,
            lowThreshold: s.low_balance_threshold,
            autoEnabled: s.auto_topup_enabled,
            autoPack: s.auto_topup_pack,
            autoThreshold: s.auto_topup_threshold,
            autoMax: s.auto_topup_max_per_month,
        });
    }, [settings.data]);

    const submit = async () => {
        if (!form) return;
        try {
            await toast.promise(
                save.mutateAsync({
                    spend_limit_daily: form.daily,
                    spend_limit_weekly: form.weekly,
                    spend_limit_monthly: form.monthly,
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

                    <UsageChart points={u.series} />

                    <div className="grid grid-cols-1 md:grid-cols-2 gap-x-8 gap-y-4">
                        <Breakdown title="By feature" rows={u.by_reason} labelFor={(k) => REASON_LABELS[k] ?? k} />
                        <Breakdown title="By model" rows={u.by_model} labelFor={(k) => k || "unmetered"} />
                    </div>
                </>
            ) : (
                <p className="text-[11.5px] text-slate-500">Usage is unavailable right now.</p>
            )}

            {form && (
                <div className="mt-2 border-t border-slate-200 pt-4 space-y-4">
                    <div>
                        <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 mb-2">Spend limits</div>
                        <div className="grid grid-cols-1 sm:grid-cols-3 gap-2">
                            <LimitField label="Per day" value={form.daily} onChange={(v) => setForm({ ...form, daily: v })} />
                            <LimitField label="Per week" value={form.weekly} onChange={(v) => setForm({ ...form, weekly: v })} />
                            <LimitField label="Per month" value={form.monthly} onChange={(v) => setForm({ ...form, monthly: v })} />
                        </div>
                        <p className="mt-1.5 text-[11px] text-slate-400">
                            When a limit is reached, AI features pause until the window rolls over. Leave a limit off for no cap.
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
                        {form.autoEnabled && (
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
                        )}
                    </div>

                    <div className="flex justify-end">
                        <button
                            type="button"
                            onClick={submit}
                            disabled={save.isPending}
                            className="h-8 px-3.5 rounded-md bg-sky-600 text-[12px] font-medium text-white hover:bg-sky-700 inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                        >
                            {save.isPending && <Loader2Icon className="w-3 h-3 animate-spin" />}
                            Save spend controls
                        </button>
                    </div>
                </div>
            )}
        </Section>
    );
}

// SpendStat is one window's spent-vs-limit chip with a progress bar when a
// limit is configured.
function SpendStat({ label, spent, limit }: { label: string; spent: number; limit: number | null }) {
    const pct = limit && limit > 0 ? Math.min(100, Math.round((spent / limit) * 100)) : null;
    return (
        <div className="rounded-md border border-slate-200 bg-white px-3 py-2">
            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400">{label}</div>
            <div className="mt-0.5 flex items-baseline gap-1">
                <span className="text-[16px] font-semibold text-slate-900 tabular-nums">{spent.toLocaleString()}</span>
                <span className="text-[11px] text-slate-400">{limit ? `/ ${limit.toLocaleString()} credits` : "credits"}</span>
            </div>
            {pct !== null && (
                <div className="mt-1.5 h-1 rounded-full bg-slate-100">
                    <div
                        className={`h-1 rounded-full ${pct >= 90 ? "bg-rose-500" : pct >= 70 ? "bg-amber-400" : "bg-sky-500"}`}
                        style={{ width: `${pct}%` }}
                    />
                </div>
            )}
        </div>
    );
}

// UsageChart is a dependency-free daily bar chart of the last 30 days.
function UsageChart({ points }: { points: { date: string; credits: number; tokens: number }[] }) {
    const byDate = new Map(points.map((p) => [p.date, p]));
    const days: { date: string; credits: number; tokens: number }[] = [];
    const now = new Date();
    for (let i = 29; i >= 0; i--) {
        const d = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate() - i));
        const key = d.toISOString().slice(0, 10);
        days.push(byDate.get(key) ?? { date: key, credits: 0, tokens: 0 });
    }
    const max = Math.max(1, ...days.map((d) => d.credits));
    return (
        <div>
            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 mb-2">Last 30 days</div>
            <div className="flex h-16 items-end gap-[2px]">
                {days.map((d) => (
                    <div
                        key={d.date}
                        title={`${d.date}: ${d.credits.toLocaleString()} credits · ${d.tokens.toLocaleString()} tokens`}
                        className="flex-1 rounded-t bg-sky-200 hover:bg-sky-400 transition-colors"
                        style={{ height: `${Math.max(d.credits > 0 ? 6 : 2, Math.round((d.credits / max) * 100))}%` }}
                    />
                ))}
            </div>
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
                            <div className="mt-0.5 h-1 rounded-full bg-slate-100">
                                <div className="h-1 rounded-full bg-sky-500" style={{ width: `${Math.round((r.credits / total) * 100)}%` }} />
                            </div>
                        </div>
                    ))}
                </div>
            )}
        </div>
    );
}

// LimitField is one spend-limit control: an on/off toggle plus the amount.
function LimitField({ label, value, onChange }: { label: string; value: number | null; onChange: (v: number | null) => void }) {
    const on = value !== null;
    return (
        <div className="rounded-md border border-slate-200 bg-white px-3 py-2">
            <div className="flex items-center justify-between">
                <span className="text-[11.5px] font-medium text-slate-700">{label}</span>
                <button
                    type="button"
                    role="switch"
                    aria-checked={on}
                    aria-label={`${label} limit`}
                    onClick={() => onChange(on ? null : 200)}
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
            {on && (
                <div className="mt-1.5 flex items-center gap-1.5">
                    <NumberInput
                        value={value}
                        onChange={(n) => onChange(Math.max(1, Math.round(n)))}
                        min={1}
                        max={1000000}
                        className="w-full"
                    />
                    <span className="text-[11px] text-slate-400">credits</span>
                </div>
            )}
        </div>
    );
}
