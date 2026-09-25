import { useState } from "react";
import { Link } from "react-router-dom";
import { AnimatePresence, motion } from "framer-motion";
import { ChevronDownIcon, ClockIcon, UsersIcon } from "lucide-react";
import useCampaignSendPlan from "@/lib/api/hooks/app/campaigns/useCampaignSendPlan";
import type SendPlan from "@/lib/api/models/app/campaigns/SendPlan";
import type { MailboxPlan, SendBottleneck, SendLimitKind } from "@/lib/api/models/app/campaigns/SendPlan";
import { DitherMeter } from "@/components/ui/dither";
import AnimatedNumber from "@/components/ui/AnimatedNumber";

// One line per limit: what it is called, what it did, and where to change it.
// `to` is relative to the campaign (settings, schedule, leads) or absolute.
interface LimitMeta {
    label: string;
    hint: string;
    to?: string;
}

const LIMIT_META: Record<SendLimitKind, LimitMeta> = {
    campaign_daily_limit: {
        label: "Campaign daily limit",
        hint: "This campaign caps each mailbox below the mailbox's own daily cap.",
        to: "preferences#senders",
    },
    campaign_ramp: {
        label: "Campaign ramp-up",
        hint: "Daily ramp-up is still climbing toward its ceiling.",
        to: "preferences#rotation",
    },
    warmup_graduation: {
        label: "Easing out of warmup",
        hint: "A mailbox that warmed starts cold at 5 to 20 a day and adds 5 each clean day until it reaches its cap.",
        to: "/app/emails",
    },
    workspace_risk: {
        label: "Workspace sending posture",
        hint: "The workspace is restricted, so every mailbox sends a fraction of its cap.",
        to: "/app/deliverability",
    },
    domain_auth: {
        label: "Domain authentication failing",
        hint: "A sending domain has failed SPF or DMARC for longer than the grace period; nothing is sent from it.",
        to: "/app/emails",
    },
    resting: {
        label: "Resting or in reserve",
        hint: "A mailbox is out of cold rotation; it keeps its warmup and sends nothing cold.",
        to: "/app/emails",
    },
    warmup_health_hold: {
        label: "Held by warmup health",
        hint: "A mailbox is quarantined or blocked by its warmup health and sends nothing cold until that lifts.",
        to: "/app/emails",
    },
    other_campaigns: {
        label: "Used by other campaigns",
        hint: "A mailbox's daily cap is shared by every campaign it is on; these sends went to another one today.",
        to: "/app/campaigns",
    },
    warmup_health_pace: {
        label: "Slowed by warmup health",
        hint: "A mailbox on watch or throttled has its sends spaced wider today.",
        to: "/app/emails",
    },
    mailbox_hours: {
        label: "Outside the mailbox's hours",
        hint: "A mailbox in another timezone, or with its own workday, is closed for the rest of today.",
        to: "/app/emails",
    },
    sending_behavior: {
        label: "Sending behaviour plan",
        hint: "A mailbox's rolled workday gives it fewer sends today than its cap.",
        to: "/app/emails",
    },
    spacing: {
        label: "Minimum gap between sends",
        hint: "With the sending time left today, the gap between two sends from one mailbox does not fit any more.",
        to: "/app/emails",
    },
    sending_window: {
        label: "Outside the sending window",
        hint: "The campaign's schedule has no sending time left today.",
        to: "schedule",
    },
    not_running: {
        label: "Campaign not running",
        hint: "The campaign is not active, so nothing goes out until it is started.",
    },
    org_daily_limit: {
        label: "Plan's daily allowance",
        hint: "The workspace's plan caps campaign emails per day across every campaign.",
        to: "/app/settings/billing",
    },
    new_lead_cap: {
        label: "New leads per day",
        hint: "Only this many contacts may receive their first email today; follow-ups keep going.",
        to: "preferences#leadflow",
    },
    leads: {
        label: "Not enough leads due",
        hint: "The mailboxes could send more, but no more steps are due today.",
        to: "leads",
    },
};

const STATE_META: Record<MailboxPlan["state"], { label: string; tone: string }> = {
    sending: { label: "Sending", tone: "bg-emerald-50 text-emerald-700 ring-emerald-200" },
    budget_spent: { label: "Budget used", tone: "bg-slate-100 text-slate-600 ring-slate-200" },
    hours_closed: { label: "Closed", tone: "bg-amber-50 text-amber-700 ring-amber-200" },
    no_working_day: { label: "Day off", tone: "bg-slate-100 text-slate-600 ring-slate-200" },
    domain_auth: { label: "Auth failing", tone: "bg-rose-50 text-rose-700 ring-rose-200" },
    resting: { label: "Resting", tone: "bg-slate-100 text-slate-600 ring-slate-200" },
    health_hold: { label: "Health hold", tone: "bg-rose-50 text-rose-700 ring-rose-200" },
    no_worker: { label: "Reconnecting", tone: "bg-amber-50 text-amber-700 ring-amber-200" },
    window_closed: { label: "Window closed", tone: "bg-amber-50 text-amber-700 ring-amber-200" },
};

const LIMITED_BY: Record<MailboxPlan["limited_by"], string> = {
    mailbox_daily_cap: "mailbox cap",
    campaign_daily_limit: "campaign limit",
    campaign_ramp: "ramp-up",
    warmup_graduation: "easing out of warmup",
    workspace_risk: "workspace posture",
};

function fmtTime(iso: string | undefined, tz: string): string {
    if (!iso) return "";
    try {
        return new Date(iso).toLocaleTimeString("en-US", { hour: "2-digit", minute: "2-digit", timeZone: tz });
    } catch {
        return new Date(iso).toLocaleTimeString("en-US", { hour: "2-digit", minute: "2-digit" });
    }
}

function fmtDay(iso: string | undefined, tz: string): string {
    if (!iso) return "";
    const d = new Date(iso);
    const today = new Date();
    const opts: Intl.DateTimeFormatOptions = { timeZone: tz };
    const sameDay = d.toLocaleDateString("en-US", opts) === today.toLocaleDateString("en-US", opts);
    if (sameDay) return fmtTime(iso, tz);
    return `${d.toLocaleDateString("en-US", { weekday: "short", timeZone: tz })} ${fmtTime(iso, tz)}`;
}

function shortTz(tz: string): string {
    const city = tz.split("/").pop() ?? tz;
    return city.replace(/_/g, " ");
}

// The one sentence under the number: what decided it.
function headline(plan: SendPlan): string {
    const b: SendBottleneck = plan.bottleneck;
    const n = plan.projected_today;
    if (plan.mailboxes.length === 0) return "No mailbox is attached to this campaign, so nothing can go out.";
    if (plan.status !== "active") {
        // What the pool would send: the not-running clamp put back.
        const held = plan.limits.find((l) => l.kind === "not_running")?.emails ?? 0;
        return `Once started, this campaign would send about ${(plan.expected_remaining + held).toLocaleString()} today.`;
    }
    switch (b) {
        case "":
            return n === plan.configured_ceiling
                ? "Nothing is holding it below your settings."
                : "Every mailbox is sending at its cap.";
        case "budget_spent":
            return "Every mailbox has used its budget for today; sending resumes tomorrow.";
        case "leads":
            return `Only ${(plan.leads.due_now + plan.leads.due_later_today).toLocaleString()} steps are due today; the mailboxes could send more.`;
        case "sending_window":
            return plan.window.opens_at
                ? `Outside the sending window; it opens ${fmtDay(plan.window.opens_at, plan.timezone)}.`
                : "Outside the sending window.";
        case "new_lead_cap":
            return `The new-leads-per-day limit (${plan.leads.max_new_leads_per_day}) is what holds it; follow-ups keep going.`;
        case "org_daily_limit":
            return `Your plan allows ${plan.organization?.daily_limit.toLocaleString() ?? ""} campaign emails a day across the workspace.`;
        default: {
            const meta = LIMIT_META[b];
            return meta ? `${meta.label} is holding it below the ${plan.configured_ceiling.toLocaleString()} your settings allow.` : "";
        }
    }
}

function WindowLine({ plan }: { plan: SendPlan }) {
    const w = plan.window;
    const tz = shortTz(plan.timezone);
    let text: string;
    if (w.starts_at && new Date(w.starts_at).getTime() > Date.now()) {
        text = `Starts ${fmtDay(w.starts_at, plan.timezone)}`;
    } else if (!w.sending_day) {
        text = w.opens_at ? `Not a sending day. Opens ${fmtDay(w.opens_at, plan.timezone)}` : "Not a sending day";
    } else if (w.open_now) {
        text = w.closes_at ? `Window open until ${fmtTime(w.closes_at, plan.timezone)}` : "Window open";
    } else if (w.opens_at) {
        text = `Window opens ${fmtDay(w.opens_at, plan.timezone)}`;
    } else {
        text = "Window closed for today";
    }
    return (
        <span className="inline-flex items-center gap-1.5 text-[11px] text-slate-500">
            <ClockIcon className="size-3 text-slate-400" />
            {text}
            <span className="text-slate-400">· {tz}</span>
        </span>
    );
}

function LeadsLine({ plan }: { plan: SendPlan }) {
    const l = plan.leads;
    const parts: string[] = [];
    parts.push(`${l.due_now.toLocaleString()} due now`);
    if (l.due_later_today) parts.push(`${l.due_later_today.toLocaleString()} later today`);
    if (l.waiting_on_step) parts.push(`${l.waiting_on_step.toLocaleString()} waiting on a step`);
    if (l.waiting_on_condition) parts.push(`${l.waiting_on_condition.toLocaleString()} in a branch window`);
    if (l.waiting_on_sender) parts.push(`${l.waiting_on_sender.toLocaleString()} waiting for their own mailbox`);
    if (l.held) parts.push(`${l.held.toLocaleString()} held`);
    if (l.max_new_leads_per_day > 0) parts.push(`${l.new_leads_started_today}/${l.max_new_leads_per_day} new leads today`);
    return (
        <span className="inline-flex items-center gap-1.5 text-[11px] text-slate-500 min-w-0">
            <UsersIcon className="size-3 text-slate-400 shrink-0" />
            <span className="truncate">Leads: {parts.join(" · ")}</span>
        </span>
    );
}

// A payload with a list missing must not take the whole overview down with
// it: the strip is a hint above the page, not the page.
function withDefaults(plan: SendPlan): SendPlan {
    return {
        ...plan,
        limits: plan.limits ?? [],
        mailboxes: plan.mailboxes ?? [],
        window: plan.window ?? { sending_day: false, open_now: false, minutes_left: 0 },
        leads: plan.leads ?? {
            due_now: 0, due_later_today: 0, new_leads_due_today: 0, waiting_on_step: 0,
            waiting_on_condition: 0, held: 0, waiting_on_sender: 0, new_leads_started_today: 0, max_new_leads_per_day: 0,
        },
        configured_ceiling: plan.configured_ceiling ?? 0,
        projected_today: plan.projected_today ?? 0,
        sent_today: plan.sent_today ?? 0,
        expected_remaining: plan.expected_remaining ?? 0,
        bottleneck: plan.bottleneck ?? "",
        timezone: plan.timezone || "UTC",
        status: plan.status ?? "",
    };
}

export default function SendPlanCard({ campaignId }: { campaignId: string }) {
    const q = useCampaignSendPlan(campaignId);
    // One strip by default; the working is behind a toggle so the analytics
    // below it stay above the fold.
    const [open, setOpen] = useState(false);
    const plan = q.data && typeof q.data === "object" && "campaign_id" in q.data ? withDefaults(q.data) : undefined;

    if (q.isPending) {
        return (
            <div className="rounded-md border border-slate-200 bg-white h-12 px-4 flex items-center gap-3">
                <div className="h-5 w-16 bg-slate-100 rounded animate-pulse" />
                <div className="h-3 w-64 bg-slate-100 rounded animate-pulse" />
            </div>
        );
    }
    if (q.isError || !plan) {
        return (
            <div className="rounded-md border border-slate-200 bg-white h-12 px-4 flex items-center gap-2 text-[11.5px] text-slate-500">
                <span className="text-slate-700 font-medium">Today's sending plan</span>
                <span>couldn't be worked out. It retries on its own.</span>
            </div>
        );
    }

    const frac = plan.projected_today > 0 ? plan.sent_today / plan.projected_today : 0;
    const campaignBase = `/app/campaigns/${campaignId}`;
    const linkFor = (to?: string) => (!to ? undefined : to.startsWith("/") ? to : `${campaignBase}/${to}`);
    const showWaterfall = plan.limits.length > 0 || plan.sent_today > 0;

    return (
        <div className="rounded-md border border-slate-200 overflow-hidden bg-white">
            {/* The strip: number, meter, the one sentence, the window, and the toggle. */}
            <button
                type="button"
                onClick={() => setOpen((v) => !v)}
                aria-expanded={open}
                className="w-full min-h-12 px-4 py-2 flex items-center gap-3 text-left hover:bg-slate-50/60 transition-colors"
            >
                <span className="flex items-baseline gap-1.5 shrink-0">
                    <AnimatedNumber
                        value={plan.projected_today}
                        className="text-[20px] font-semibold text-slate-900 leading-none tabular-nums"
                    />
                    <span className="text-[11.5px] text-slate-600">{plan.status === "active" ? "today" : "a day"}</span>
                </span>
                <span className="hidden sm:block w-20 shrink-0">
                    <DitherMeter frac={frac} tone="sky" height={4} />
                </span>
                <span className="text-[11px] text-slate-400 font-mono tabular-nums shrink-0 hidden md:inline">
                    {plan.sent_today.toLocaleString()} sent · {plan.expected_remaining.toLocaleString()} to go
                </span>
                <span className="text-[11.5px] text-slate-500 truncate min-w-0 flex-1">{headline(plan)}</span>
                <span className="hidden lg:inline-flex shrink-0">
                    <WindowLine plan={plan} />
                </span>
                <span className="inline-flex items-center gap-1 text-[11px] text-slate-500 shrink-0">
                    Why
                    <ChevronDownIcon className={`size-3 transition-transform ${open ? "rotate-180" : ""}`} />
                </span>
            </button>

            <AnimatePresence initial={false}>
                {open ? (
                    <motion.div
                        key="why"
                        initial={{ height: 0, opacity: 0 }}
                        animate={{ height: "auto", opacity: 1 }}
                        exit={{ height: 0, opacity: 0 }}
                        transition={{ duration: 0.18, ease: "easeOut" }}
                        className="overflow-hidden"
                    >
                <div className="border-t border-slate-200/60">
                    <div className="grid md:grid-cols-2 divide-y md:divide-y-0 md:divide-x divide-slate-200/60">
                        {/* Left: from the settings to today. */}
                        <div className="px-4 py-3">
                            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium mb-1.5">
                                From your settings to today
                            </div>
                            {showWaterfall ? (
                                <div className="divide-y divide-slate-100">
                                    <div className="h-7 flex items-center gap-3 text-[12px]">
                                        <span className="text-slate-700">Mailbox caps added up</span>
                                        <span className="ml-auto font-mono text-[11.5px] text-slate-700 tabular-nums">
                                            {plan.configured_ceiling.toLocaleString()}
                                        </span>
                                    </div>
                                    {plan.limits.map((l) => {
                                        const meta = LIMIT_META[l.kind];
                                        const to = linkFor(meta?.to);
                                        const body = (
                                            <>
                                                <span className="text-slate-700 truncate">{meta?.label ?? l.kind}</span>
                                                {l.mailboxes ? (
                                                    <span className="text-[10.5px] text-slate-400 shrink-0">
                                                        {l.mailboxes} {l.mailboxes === 1 ? "mailbox" : "mailboxes"}
                                                    </span>
                                                ) : null}
                                                <span className="ml-auto font-mono text-[11.5px] text-rose-600 tabular-nums shrink-0">
                                                    −{l.emails.toLocaleString()}
                                                </span>
                                            </>
                                        );
                                        return to ? (
                                            <Link
                                                key={l.kind}
                                                to={to}
                                                title={meta?.hint}
                                                className="h-7 flex items-center gap-2 text-[12px] hover:bg-slate-50 -mx-2 px-2 rounded transition-colors"
                                            >
                                                {body}
                                            </Link>
                                        ) : (
                                            <div key={l.kind} title={meta?.hint} className="h-7 flex items-center gap-2 text-[12px]">
                                                {body}
                                            </div>
                                        );
                                    })}
                                    {plan.sent_today > 0 && (
                                        <div className="h-7 flex items-center gap-3 text-[12px]">
                                            <span className="text-slate-700">Sent today</span>
                                            <span className="ml-auto font-mono text-[11.5px] text-slate-500 tabular-nums">
                                                −{plan.sent_today.toLocaleString()}
                                            </span>
                                        </div>
                                    )}
                                    <div className="h-7 flex items-center gap-3 text-[12px]">
                                        <span className="text-slate-900 font-medium">Still to go today</span>
                                        <span className="ml-auto font-mono text-[11.5px] text-slate-900 font-medium tabular-nums">
                                            {plan.expected_remaining.toLocaleString()}
                                        </span>
                                    </div>
                                </div>
                            ) : (
                                <p className="text-[11.5px] text-slate-400 py-1">Nothing lowers the number today.</p>
                            )}
                            <div className="mt-2 flex flex-col gap-1">
                                <LeadsLine plan={plan} />
                                <span className="lg:hidden">
                                    <WindowLine plan={plan} />
                                </span>
                                {plan.organization && (
                                    <span className="text-[11px] text-slate-500">
                                        Plan allowance {plan.organization.sent_today.toLocaleString()}/{plan.organization.daily_limit.toLocaleString()} today
                                    </span>
                                )}
                                {plan.next_wake_at && (
                                    <span className="text-[11px] text-slate-400">Next pass {fmtDay(plan.next_wake_at, plan.timezone)}</span>
                                )}
                            </div>
                        </div>

                        {/* Right: each mailbox's day. */}
                        <div className="px-4 py-3 min-w-0">
                            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium mb-1.5">
                                {plan.mailboxes.length} {plan.mailboxes.length === 1 ? "mailbox" : "mailboxes"}
                            </div>
                            {plan.mailboxes.length === 0 ? (
                                <p className="text-[11.5px] text-slate-400 py-1">No mailbox is attached to this campaign.</p>
                            ) : (
                                <div className="divide-y divide-slate-100">
                                    {plan.mailboxes.map((m) => {
                                        const st = STATE_META[m.state] ?? STATE_META.sending;
                                        const sentTitle = m.sent_by_other_campaigns
                                            ? `${m.sent_today} from this campaign, ${m.sent_by_other_campaigns} from other campaigns`
                                            : `${m.sent_today} from this campaign`;
                                        return (
                                            <div key={m.id} className="py-1.5 flex items-center gap-2 min-w-0">
                                                <span className="flex-1 min-w-0">
                                                    <span className="block text-[12px] text-slate-900 truncate">{m.email}</span>
                                                    <span className="block text-[10.5px] text-slate-400 truncate">
                                                        {m.limited_by !== "mailbox_daily_cap" ? `${LIMITED_BY[m.limited_by]} · ` : ""}
                                                        gap {Math.round(m.min_gap_seconds / 60)}m
                                                        {m.graduation
                                                            ? ` · ${m.graduation.days_to_full_cap} clean ${m.graduation.days_to_full_cap === 1 ? "day" : "days"} to ${m.graduation.mailbox_cap}${m.graduation.held ? ", climb paused" : ""}`
                                                            : ""}
                                                        {m.health ? ` · warmup ${m.health}` : ""}
                                                        {m.reopens_at ? ` · reopens ${fmtDay(m.reopens_at, plan.timezone)}` : ""}
                                                    </span>
                                                </span>
                                                <span className="font-mono text-[11px] text-slate-500 tabular-nums shrink-0" title={sentTitle}>
                                                    {m.sent_today}
                                                    {m.sent_by_other_campaigns ? <span className="text-slate-400">+{m.sent_by_other_campaigns}</span> : null}
                                                    <span className="text-slate-300"> / </span>
                                                    <span className="text-slate-700">{m.cap_today}</span>
                                                    {m.cap_today !== m.configured_cap && <span className="text-slate-400"> of {m.configured_cap}</span>}
                                                </span>
                                                <span
                                                    className={`inline-flex items-center rounded px-1.5 h-5 text-[10px] font-medium ring-1 shrink-0 ${st.tone}`}
                                                    title={m.state === "sending" ? `${m.expected_remaining} still to go` : undefined}
                                                >
                                                    {st.label}
                                                </span>
                                            </div>
                                        );
                                    })}
                                </div>
                            )}
                        </div>
                    </div>
                </div>
                    </motion.div>
                ) : null}
            </AnimatePresence>
        </div>
    );
}
