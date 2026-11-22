// One social account: usage, health and capabilities, its per-action limits, hours and warm-up, and its billing.
import { useCallback, useEffect, useMemo, useState } from "react";
import { motion } from "framer-motion";
import {
    ActivityIcon,
    AlertTriangleIcon,
    CreditCardIcon,
    ExternalLinkIcon,
    Loader2Icon,
    PauseIcon,
    PlayIcon,
    RefreshCwIcon,
    RotateCcwIcon,
    SlidersHorizontalIcon,
    Trash2Icon,
    XIcon,
} from "lucide-react";
import { Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import { Label, NumberInput } from "@/components/ui/field";
import ScrollStrip from "@/components/ui/scroll-strip";
import { SelectMenu } from "@/components/ui/select-menu";
import TimeSelect from "@/components/ui/TimeSelect";
import { useConfirm } from "@/hooks/context/confirm";
import { usePatchSocialAccount, useSocialAccount } from "@/lib/api/hooks/app/social/useSocial";
import useTimezones from "@/lib/api/hooks/app/useTimezones";
import type { SocialAccount, SocialAccountPatch, SocialWorkingHours } from "@/lib/api/models/app/social/Social";
import { fmtMonthly } from "@/lib/buy/money";
import { cn } from "@/lib/utils";
import { actionLabel, channelLabel, profileUrl } from "@/lib/social/channels";
import ChannelMark from "./ChannelMark";
import { actionPlural, Avatar, BillingLine, fmtDay, LicenceChips, limitKeys, Meter, StatusPill, usageFor } from "./parts";
import useAccountActions, { canPause, canResume, needsReconnect } from "./useAccountActions";

export type DrawerTab = "overview" | "settings" | "billing";

const TABS: { id: DrawerTab; label: string; icon: typeof ActivityIcon }[] = [
    { id: "overview", label: "Overview", icon: ActivityIcon },
    { id: "settings", label: "Settings", icon: SlidersHorizontalIcon },
    { id: "billing", label: "Billing", icon: CreditCardIcon },
];

export default function AccountDrawer({
    accountId,
    initialTab,
    onClose,
    onReconnect,
}: {
    accountId: string;
    initialTab: DrawerTab;
    onClose: () => void;
    onReconnect: (a: SocialAccount) => void;
}) {
    const q = useSocialAccount(accountId);
    const account = q.data;
    const confirm = useConfirm();
    const [tab, setTab] = useState<DrawerTab>(initialTab);
    const [dirty, setDirty] = useState(false);
    useEffect(() => setTab(initialTab), [initialTab, accountId]);

    const requestClose = useCallback(() => {
        if (dirty) confirm.show("Discard the changes to this account's settings?", async () => onClose(), undefined, { title: "Discard", confirmLabel: "Discard" });
        else onClose();
    }, [dirty, onClose, confirm]);

    useEffect(() => {
        const onKey = (e: KeyboardEvent) => {
            if (e.key !== "Escape") return;
            if (document.querySelector("[data-floating], [role='alertdialog'], [aria-modal='true']")) return;
            requestClose();
        };
        window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    }, [requestClose]);

    return (
        <motion.div
            key="overlay"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.15 }}
            className="fixed inset-0 z-[110] flex justify-end bg-slate-900/30 backdrop-blur-[2px]"
            onMouseDown={requestClose}
        >
            <motion.aside
                key="panel"
                initial={{ x: 32, opacity: 0 }}
                animate={{ x: 0, opacity: 1 }}
                exit={{ x: 32, opacity: 0 }}
                transition={{ duration: 0.2, ease: [0.32, 0.72, 0, 1] }}
                onMouseDown={(e) => e.stopPropagation()}
                aria-label={account ? `${channelLabel(account.channel)} account` : "Social account"}
                className="flex flex-col w-full max-w-full md:w-[30rem] md:max-w-[95%] h-full bg-white border-l border-slate-200 shadow-[-12px_0_24px_-12px_rgba(15,23,42,0.08)]"
            >
                <header className="shrink-0 px-4 py-3 border-b border-slate-200 flex items-start gap-3">
                    {account ? (
                        <>
                            <span className="relative shrink-0">
                                <Avatar src={account.avatar_url} name={account.display_name} size={40} />
                                <ChannelMark channel={account.channel} className="absolute -bottom-0.5 -right-0.5 size-4 rounded-[4px] ring-2 ring-white" />
                            </span>
                            <div className="min-w-0 flex-1">
                                <div className="flex items-center gap-2 min-w-0">
                                    <p className="text-[13.5px] font-semibold text-slate-900 truncate">{account.display_name || account.username || `${channelLabel(account.channel)} account`}</p>
                                    {profileUrl(account.channel, account.profile_url) && (
                                        <a
                                            href={profileUrl(account.channel, account.profile_url) ?? undefined}
                                            target="_blank"
                                            rel="noopener noreferrer"
                                            aria-label={`Open on ${channelLabel(account.channel)}`}
                                            className="text-slate-400 hover:text-slate-700"
                                        >
                                            <ExternalLinkIcon className="w-3.5 h-3.5" />
                                        </a>
                                    )}
                                </div>
                                {(account.headline || account.username) && <p className="text-[11.5px] text-slate-500 truncate">{account.headline || account.username}</p>}
                                <div className="mt-1.5 flex flex-wrap items-center gap-1.5">
                                    <StatusPill status={account.status} />
                                    <LicenceChips licences={account.licences} />
                                </div>
                            </div>
                        </>
                    ) : (
                        <div className="flex-1 h-10 rounded skeleton-shimmer" />
                    )}
                    <button
                        type="button"
                        onClick={requestClose}
                        aria-label="Close"
                        className="size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center shrink-0"
                    >
                        <XIcon className="w-3.5 h-3.5" />
                    </button>
                </header>

                <ScrollStrip activeKey={tab} className="shrink-0 border-b border-slate-200" innerClassName="px-3 gap-1">
                    {TABS.map((t) => {
                        const active = tab === t.id;
                        return (
                            <button
                                key={t.id}
                                type="button"
                                data-active={active ? "true" : undefined}
                                onClick={() => setTab(t.id)}
                                className={cn(
                                    "relative h-10 px-2.5 shrink-0 inline-flex items-center gap-1.5 text-[12.5px] whitespace-nowrap outline-none transition-colors",
                                    active ? "text-slate-900 font-medium" : "text-slate-500 hover:text-slate-800",
                                )}
                            >
                                <t.icon className="w-3.5 h-3.5" />
                                {t.label}
                                {active && (
                                    <motion.span
                                        layoutId="social-account-tab-underline"
                                        className="absolute left-1.5 right-1.5 bottom-0 h-0.5 rounded-full bg-sky-600"
                                        transition={{ type: "spring", duration: 0.3, bounce: 0.15 }}
                                    />
                                )}
                            </button>
                        );
                    })}
                </ScrollStrip>

                <div className="flex-1 min-h-0 overflow-y-auto px-4 py-4">
                    {!account ? (
                        q.isError ? (
                            <p className="text-[12px] text-slate-500">This account could not be loaded. It may have been removed.</p>
                        ) : (
                            <div className="space-y-2">
                                <div className="h-16 rounded skeleton-shimmer" />
                                <div className="h-32 rounded skeleton-shimmer" />
                            </div>
                        )
                    ) : tab === "overview" ? (
                        <OverviewTab account={account} onReconnect={() => onReconnect(account)} />
                    ) : tab === "settings" ? (
                        <SettingsTab key={account.id} account={account} onDirty={setDirty} onRemoved={onClose} />
                    ) : (
                        <BillingTab account={account} />
                    )}
                </div>
            </motion.aside>
        </motion.div>
    );
}

function Section({ label, children, className }: { label: string; children: React.ReactNode; className?: string }) {
    return (
        <section className={cn("space-y-2", className)}>
            <h3 className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">{label}</h3>
            {children}
        </section>
    );
}

function Fact({ label, value }: { label: string; value: React.ReactNode }) {
    return (
        <div className="flex items-baseline justify-between gap-3 py-1.5 border-b border-slate-100 last:border-0">
            <span className="text-[12px] text-slate-500">{label}</span>
            <span className="text-[12px] text-slate-900 text-right min-w-0 truncate">{value}</span>
        </div>
    );
}

const WEEKDAY = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];

function hoursSummary(h: SocialWorkingHours | null, tz: string): string {
    if (!h) return "Any time";
    const days = [...h.days].sort().map((d) => WEEKDAY[d - 1]).filter(Boolean);
    const span = days.length === 7 ? "Every day" : days.length === 5 && !h.days.includes(6) && !h.days.includes(7) ? "Weekdays" : days.join(", ");
    return `${span}, ${h.start} to ${h.end}${tz ? ` (${tz})` : ""}`;
}

function OverviewTab({ account, onReconnect }: { account: SocialAccount; onReconnect: () => void }) {
    const actions = useAccountActions();
    const invite = usageFor(account, "invite");
    const acceptance = account.acceptance_rate;
    const unsupported = Object.entries(account.capabilities ?? {}).filter(([, c]) => c && !c.supported);
    const linkedin = account.channel === "linkedin";
    return (
        <div className="space-y-5">
            {needsReconnect(account) && (
                <div className="rounded-lg border border-amber-200 bg-amber-50/60 px-3 py-2.5 flex items-start gap-2.5">
                    <AlertTriangleIcon className="w-4 h-4 text-amber-600 mt-0.5 shrink-0" />
                    <div className="min-w-0 flex-1">
                        <p className="text-[12px] font-medium text-amber-950">{account.status === "restricted" ? `${channelLabel(account.channel)} restricted this account` : "This account needs to sign in again"}</p>
                        <p className="text-[11.5px] text-amber-900 leading-snug">
                            {account.status_reason ?? "Nothing runs on it until it does. Sign in again to pick up where it left off."}
                        </p>
                    </div>
                    {account.status !== "restricted" && (
                        <button type="button" onClick={onReconnect} className="h-7 px-2.5 rounded-md bg-amber-600 hover:bg-amber-700 text-white text-[12px] font-medium inline-flex items-center gap-1 shrink-0">
                            <RotateCcwIcon className="w-3 h-3" /> Reconnect
                        </button>
                    )}
                </div>
            )}

            <Section label="Today">
                <div className="grid grid-cols-2 gap-x-4 gap-y-3">
                    {limitKeys(account).map((a) => (
                        <Meter key={a} usage={usageFor(account, a)} />
                    ))}
                    {invite.weekly_limit ? (
                        <Meter usage={{ action: "invite", today: invite.week ?? 0, daily_limit: invite.weekly_limit }} />
                    ) : null}
                </div>
                {invite.weekly_limit ? <p className="text-[11px] text-slate-400">The last meter is invites this week, against the weekly limit.</p> : null}
            </Section>

            {unsupported.length > 0 && (
                <Section label="Not available on this account">
                    <ul className="rounded-lg border border-slate-200 divide-y divide-slate-100">
                        {unsupported.map(([action, c]) => (
                            <li key={action} className="px-3 py-2">
                                <p className="text-[12px] text-slate-800">{actionLabel(action)}</p>
                                {c?.reason && <p className="text-[11px] text-slate-500 leading-snug">{c.reason}</p>}
                            </li>
                        ))}
                    </ul>
                    <p className="text-[11px] text-slate-400">Campaign steps that need these skip contacts on this account.</p>
                </Section>
            )}

            {account.voice && (
                <Section label="Voice">
                    <p className="text-[12px] text-slate-600 leading-relaxed">
                        {account.voice.available
                            ? "This account has a voice, so voice note steps can send a written script spoken in it."
                            : "This account has no voice yet. Voice note steps send recordings only; a written script needs a voice set up in GraphKit."}
                    </p>
                </Section>
            )}

            <Section label="Health">
                <div className="rounded-lg border border-slate-200 px-3">
                    <Fact label="Status" value={<StatusPill status={account.status} />} />
                    {linkedin && account.pending_invites != null && <Fact label="Invites waiting for an answer" value={account.pending_invites.toLocaleString()} />}
                    {linkedin && acceptance != null && <Fact label="Invites accepted" value={`${Math.round(acceptance <= 1 ? acceptance * 100 : acceptance)}%`} />}
                    {linkedin && account.inmail_credits && (
                        <Fact
                            label="InMail credits"
                            value={
                                [
                                    account.inmail_credits.sales_navigator != null && `${account.inmail_credits.sales_navigator} Sales Navigator`,
                                    account.inmail_credits.recruiter != null && `${account.inmail_credits.recruiter} Recruiter`,
                                    account.inmail_credits.classic != null && `${account.inmail_credits.classic} Premium`,
                                ]
                                    .filter(Boolean)
                                    .join(" · ") || "None"
                            }
                        />
                    )}
                    <Fact
                        label="Warm-up"
                        value={
                            account.warmup?.enabled
                                ? `${account.warmup.percent ?? 0}% of limits${account.warmup.ends_on ? `, full on ${fmtDay(account.warmup.ends_on)}` : ""}`
                                : "Off"
                        }
                    />
                    <Fact label="Working hours" value={hoursSummary(account.working_hours, account.timezone)} />
                    {account.connected_at && <Fact label="Connected" value={fmtDay(account.connected_at)} />}
                </div>
            </Section>

            <div className="flex flex-wrap gap-1.5">
                {canPause(account) && (
                    <button type="button" onClick={() => actions.pause(account)} className={ghost}>
                        <PauseIcon className="w-3 h-3" /> Pause
                    </button>
                )}
                {canResume(account) && (
                    <button type="button" onClick={() => actions.resume(account)} className={ghost}>
                        <PlayIcon className="w-3 h-3" /> Resume
                    </button>
                )}
                <button type="button" onClick={() => actions.resync(account)} className={ghost}>
                    <RefreshCwIcon className="w-3 h-3" /> Resync from {channelLabel(account.channel)}
                </button>
            </div>
        </div>
    );
}

const ghost =
    "h-7 px-2.5 rounded-md border border-slate-200 bg-white hover:bg-slate-50 text-slate-700 text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors";

interface Draft {
    limits: Record<string, number>;
    weekly: Record<string, number>;
    days: number[];
    start: string;
    end: string;
    timezone: string;
    warmup: boolean;
    warmupWeeks: number;
    withdraw: number;
}

function weeklyKeys(a: SocialAccount): string[] {
    return [...new Set([...Object.keys(a.weekly_limits ?? {}), ...Object.keys(a.weekly_limit_ceilings ?? {})])];
}

function draftOf(a: SocialAccount): Draft {
    const limits: Record<string, number> = {};
    for (const k of limitKeys(a)) limits[k] = a.limits?.[k] ?? 0;
    const weekly: Record<string, number> = {};
    for (const k of weeklyKeys(a)) weekly[k] = a.weekly_limits?.[k] ?? 0;
    return {
        limits,
        weekly,
        days: a.working_hours?.days ?? [1, 2, 3, 4, 5],
        start: a.working_hours?.start ?? "09:00",
        end: a.working_hours?.end ?? "17:00",
        timezone: a.timezone || "UTC",
        warmup: a.warmup?.enabled ?? false,
        warmupWeeks: a.warmup?.weeks ?? 3,
        withdraw: a.withdraw_invites_after_days ?? 21,
    };
}

function SettingsTab({ account, onDirty, onRemoved }: { account: SocialAccount; onDirty: (d: boolean) => void; onRemoved: () => void }) {
    const patch = usePatchSocialAccount();
    const actions = useAccountActions();
    const timezones = useTimezones();
    const base = useMemo(() => draftOf(account), [account]);
    const [draft, setDraft] = useState<Draft>(base);
    const dirty = JSON.stringify(draft) !== JSON.stringify(base);
    useEffect(() => onDirty(dirty), [dirty, onDirty]);
    useEffect(() => () => onDirty(false), [onDirty]);

    const ceilings = account.limit_ceilings ?? {};
    const weeklyCeilings = account.weekly_limit_ceilings ?? {};
    const linkedin = account.channel === "linkedin";
    const tzOptions = useMemo(() => {
        const list = (timezones.data ?? []).map((t) => ({ value: t.name, label: t.display_name }));
        return list.some((o) => o.value === draft.timezone) ? list : [{ value: draft.timezone, label: draft.timezone }, ...list];
    }, [timezones.data, draft.timezone]);

    const hoursInvalid = draft.start >= draft.end;
    const noDays = draft.days.length === 0;
    const set = (p: Partial<Draft>) => setDraft((d) => ({ ...d, ...p }));

    const save = () => {
        const body: SocialAccountPatch = {
            limits: { ...draft.limits },
            ...(Object.keys(draft.weekly).length > 0 ? { weekly_limits: { ...draft.weekly } } : {}),
            working_hours: { days: [...draft.days].sort(), start: draft.start, end: draft.end },
            timezone: draft.timezone,
            warmup: { enabled: draft.warmup, weeks: draft.warmupWeeks },
            ...(linkedin ? { withdraw_invites_after_days: draft.withdraw } : {}),
        };
        patch.mutate({ id: account.id, patch: body });
    };

    return (
        <div className="space-y-5 pb-16">
            <Section label="Daily limits">
                <p className="text-[11.5px] text-slate-500 leading-relaxed">
                    Each limit is capped at what is safe for this account{linkedin ? "'s licence" : ` on ${channelLabel(account.channel)}`}.
                </p>
                <div className="grid grid-cols-2 gap-3">
                    {Object.keys(draft.limits).map((k) => {
                        const max = ceilings[k];
                        return (
                            <div key={k}>
                                <Label htmlFor={`social-limit-${k}`}>{actionPlural(k)} per day</Label>
                                <NumberInput
                                    id={`social-limit-${k}`}
                                    value={draft.limits[k]}
                                    onChange={(v) => set({ limits: { ...draft.limits, [k]: v } })}
                                    min={0}
                                    max={max}
                                    suffix={max !== undefined ? <span className="text-slate-400">/ {max}</span> : undefined}
                                    className="w-full"
                                />
                            </div>
                        );
                    })}
                    {Object.keys(draft.weekly).map((k) => {
                        const max = weeklyCeilings[k];
                        return (
                            <div key={`week-${k}`}>
                                <Label htmlFor={`social-weekly-${k}`}>{actionPlural(k)} per week</Label>
                                <NumberInput
                                    id={`social-weekly-${k}`}
                                    value={draft.weekly[k]}
                                    onChange={(v) => set({ weekly: { ...draft.weekly, [k]: v } })}
                                    min={0}
                                    max={max}
                                    suffix={max !== undefined ? <span className="text-slate-400">/ {max}</span> : undefined}
                                    className="w-full"
                                />
                            </div>
                        );
                    })}
                </div>
            </Section>

            <Section label="Working hours">
                <div className="flex flex-wrap gap-1">
                    {WEEKDAY.map((d, i) => {
                        const n = i + 1;
                        const on = draft.days.includes(n);
                        return (
                            <button
                                key={d}
                                type="button"
                                aria-pressed={on}
                                onClick={() => set({ days: on ? draft.days.filter((x) => x !== n) : [...draft.days, n] })}
                                className={cn(
                                    "h-7 w-10 rounded-md border text-[12px] transition-colors",
                                    on ? "border-sky-300 bg-sky-50 text-sky-800 font-medium" : "border-slate-200 text-slate-500 hover:border-slate-300",
                                )}
                            >
                                {d}
                            </button>
                        );
                    })}
                </div>
                <div className="grid grid-cols-2 gap-3">
                    <div>
                        <Label>From</Label>
                        <TimeSelect value={draft.start} onChange={(v) => set({ start: v })} />
                    </div>
                    <div>
                        <Label>Until</Label>
                        <TimeSelect value={draft.end} onChange={(v) => set({ end: v })} />
                    </div>
                </div>
                <div>
                    <Label>Timezone</Label>
                    <SelectMenu value={draft.timezone} onChange={(v) => set({ timezone: v })} options={tzOptions} fullWidth aria-label="Timezone" />
                </div>
                {hoursInvalid && <p className="text-[11.5px] text-red-600">The end has to be later than the start.</p>}
                {noDays && <p className="text-[11.5px] text-red-600">Pick at least one day.</p>}
            </Section>

            <Section label="Warm-up">
                <div className="rounded-lg border border-slate-200 px-3 py-2.5 flex items-start gap-2.5">
                    <Toggle value={draft.warmup} onChange={(v) => set({ warmup: v })} ariaLabel="Warm up this account" />
                    <div className="min-w-0 flex-1">
                        <p className="text-[12px] text-slate-800">Start below the limits and ramp up</p>
                        <p className="text-[11px] text-slate-500 leading-snug">A fresh or quiet account does less at first, so {channelLabel(account.channel)} sees its activity grow gradually.</p>
                        {draft.warmup && (
                            <div className="mt-2 w-40">
                                <Label htmlFor="social-warmup-weeks">Weeks to full limits</Label>
                                <NumberInput id="social-warmup-weeks" value={draft.warmupWeeks} onChange={(v) => set({ warmupWeeks: v })} min={1} max={8} className="w-full" />
                            </div>
                        )}
                    </div>
                </div>
            </Section>

            {linkedin && (
            <Section label="Invites">
                <div className="w-48">
                    <Label htmlFor="social-withdraw">Withdraw unanswered invites after</Label>
                    <NumberInput
                        id="social-withdraw"
                        value={draft.withdraw}
                        onChange={(v) => set({ withdraw: v })}
                        min={7}
                        max={90}
                        suffix={<span className="text-slate-400">days</span>}
                        className="w-full"
                    />
                </div>
                <p className="text-[11px] text-slate-500 leading-snug">Old pending invites count against LinkedIn&apos;s limits, so they are withdrawn after this long.</p>
            </Section>
            )}

            <Section label="Danger zone">
                <div className="rounded-lg border border-red-200 px-3 py-2.5 flex items-center gap-3">
                    <div className="min-w-0 flex-1">
                        <p className="text-[12px] text-slate-800 font-medium">Remove this account</p>
                        <p className="text-[11px] text-slate-500 leading-snug">Stops everything it runs and disconnects it from Warmbly.</p>
                    </div>
                    <button
                        type="button"
                        onClick={() => actions.remove(account, onRemoved)}
                        className="h-7 px-2.5 rounded-md border border-red-200 text-red-700 hover:bg-red-50 text-[12px] font-medium inline-flex items-center gap-1.5 shrink-0"
                    >
                        <Trash2Icon className="w-3 h-3" /> Remove
                    </button>
                </div>
            </Section>

            {dirty && (
                <div className="sticky bottom-0 -mx-4 px-4 py-2.5 border-t border-slate-200 bg-white/95 backdrop-blur flex items-center justify-end gap-2">
                    {(hoursInvalid || noDays) && <span className="mr-auto text-[11.5px] text-slate-500">Fix the working hours to save.</span>}
                    <button type="button" onClick={() => setDraft(base)} className={ghost}>
                        Discard
                    </button>
                    <button
                        type="button"
                        disabled={hoursInvalid || noDays || patch.isPending}
                        onClick={save}
                        className="h-7 px-3 rounded-md bg-sky-600 hover:bg-sky-700 disabled:bg-slate-200 disabled:text-slate-400 text-white text-[12.5px] font-medium inline-flex items-center gap-1.5"
                    >
                        {patch.isPending && <Loader2Icon className="w-3.5 h-3.5 animate-spin" />}
                        Save changes
                    </button>
                </div>
            )}
        </div>
    );
}

function BillingTab({ account }: { account: SocialAccount }) {
    const b = account.billing;
    if (!b) return <p className="text-[12px] text-slate-500">No billing details for this account.</p>;
    if (b.mode === "linked") {
        return (
            <div className="space-y-3">
                <div className="rounded-lg border border-slate-200 px-3">
                    <Fact label="Billed by" value="Your GraphKit workspace" />
                    {account.connected_at && <Fact label="Connected" value={fmtDay(account.connected_at)} />}
                </div>
                <p className="text-[11.5px] text-slate-500 leading-relaxed">Warmbly never charges for accounts in a linked GraphKit workspace. They are on your GraphKit plan.</p>
            </div>
        );
    }
    if (!b.monthly_micros && !b.unpaid_since) {
        return (
            <div className="space-y-3">
                <div className="rounded-lg border border-slate-200 px-3">
                    <Fact label="Price" value="Not charged" />
                    {account.connected_at && <Fact label="Connected" value={fmtDay(account.connected_at)} />}
                </div>
                <p className="text-[11.5px] text-slate-500 leading-relaxed">Nothing is taken from your balance or card for this account.</p>
            </div>
        );
    }
    return (
        <div className="space-y-3">
            {b.unpaid_since && (
                <div className="rounded-lg border border-red-200 bg-red-50/70 px-3 py-2.5 text-[11.5px] text-red-800 leading-relaxed">
                    The monthly payment for this account did not go through on {fmtDay(b.unpaid_since)}. Add money to your balance or update the card under Billing; the account
                    pauses if it stays unpaid.
                </div>
            )}
            <div className="rounded-lg border border-slate-200 px-3">
                <Fact label="Price" value={b.monthly_micros ? fmtMonthly(b.monthly_micros) : "Included"} />
                <Fact label="Paid through" value={b.paid_through ? fmtDay(b.paid_through) : "Not yet"} />
                <Fact label="Paid with" value="Balance first, then the card on file" />
                {account.connected_at && <Fact label="Connected" value={fmtDay(account.connected_at)} />}
            </div>
            <BillingLine account={account} />
            <p className="text-[11.5px] text-slate-500 leading-relaxed">
                Each month the account renews from your balance, and the card on file pays what the balance does not cover. Removing the account stops the renewal; the month already
                paid is not refunded.
            </p>
        </div>
    );
}
