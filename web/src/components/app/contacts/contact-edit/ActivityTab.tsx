// Activity tab — the contact 360 timeline.
//
// Layout:
//   - Campaign panel: for each campaign the contact is in, the flow with this
//     contact's progress, the lead status, and a scheduler-backed next action
//   - Filters: type chips (All / Emails / Replies / Deliv. / Notes /
//     Meetings / Campaigns / Lifecycle / Website), free-text query, date range
//   - Feed: collapsed rows stay to one line; click a row to expand every
//     detail the event carries
//
// Filtering is client-side over whatever pages are in the cache.
// Pagination is server-side cursor pagination (page size 50). The hook
// returns useInfiniteQuery state; this view auto-fetches the next page when
// the sentinel scrolls into view. When the user narrows the visible set
// with a filter, we lazy-prefetch a few more pages so the filtered list
// isn't artificially short. Both queries live under ["contacts", id], so
// the audit spine's contact invalidation keeps them live.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { DatePicker } from "@/components/ui/DatePicker";
import {
    ClipboardListIcon,
    AlertOctagonIcon,
    BanIcon,
    CalendarIcon,
    CalendarClockIcon,
    CalendarPlusIcon,
    CalendarXIcon,
    CheckIcon,
    ChevronDownIcon,
    ClockIcon,
    Loader2Icon,
    MailIcon,
    MailOpenIcon,
    MailWarningIcon,
    MegaphoneIcon,
    MessageSquareIcon,
    MousePointerClickIcon,
    OctagonXIcon,
    PauseIcon,
    ReplyIcon,
    SearchIcon,
    StickyNoteIcon,
    TagIcon,
    UserPlusIcon,
    XIcon,
    GlobeIcon,
} from "lucide-react";
import useContactTimeline from "@/lib/api/hooks/app/contacts/useContactTimeline";
import useContactCampaignStates from "@/lib/api/hooks/app/contacts/useContactCampaignStates";
import type ContactTimelineEvent from "@/lib/api/models/app/contacts/ContactTimelineEvent";
import type {
    ContactTimelineEventType,
    EngagementOrigin,
} from "@/lib/api/models/app/contacts/ContactTimelineEvent";
import type ContactCampaignState from "@/lib/api/models/app/contacts/ContactCampaignState";
import type {
    ContactCampaignStep,
    ContactNextAction,
} from "@/lib/api/models/app/contacts/ContactCampaignState";
import type { LeadStatus } from "@/lib/api/models/app/contacts/Contact";
import useClickOutside from "@/hooks/useClickOutside";
import { useFlipAlignment } from "@/hooks/useFlipPlacement";
import { fmtAbsolute, fmtRelative } from "./format";

type FilterId =
    | "all"
    | "emails"
    | "replies"
    | "deliv"
    | "notes"
    | "meetings"
    | "campaigns"
    | "lifecycle"
    | "website";

const FILTERS: { id: FilterId; label: string }[] = [
    { id: "all", label: "All" },
    { id: "emails", label: "Emails" },
    { id: "replies", label: "Replies" },
    { id: "deliv", label: "Deliv." },
    { id: "notes", label: "Notes" },
    { id: "meetings", label: "Meetings" },
    { id: "campaigns", label: "Campaigns" },
    { id: "lifecycle", label: "Lifecycle" },
    { id: "website", label: "Website" },
];

const EMAIL_TYPES: ContactTimelineEventType[] = [
    "email_sent",
    "email_opened",
    "email_clicked",
    "email_bounced",
];

const MEETING_TYPES: ContactTimelineEventType[] = [
    "meeting_booked",
    "meeting_rescheduled",
    "meeting_canceled",
];

const CAMPAIGN_TYPES: ContactTimelineEventType[] = [
    "campaign_added",
    "campaign_removed",
];

const LIFECYCLE_TYPES: ContactTimelineEventType[] = [
    "contact_created",
    "category_added",
    "category_removed",
    "form_submitted",
    ...CAMPAIGN_TYPES,
];

export default function ActivityTab({ contactId }: { contactId: string }) {
    const {
        events,
        isLoading,
        isFetchingNextPage,
        error,
        hasNextPage,
        fetchNextPage,
    } = useContactTimeline(contactId);

    const [type, setType] = React.useState<FilterId>("all");
    const [query, setQuery] = React.useState("");
    const [from, setFrom] = React.useState("");
    const [to, setTo] = React.useState("");

    const visible = React.useMemo(
        () => applyFilters(events, { type, query, from, to }),
        [events, type, query, from, to],
    );

    // Auto-load: keep prefetching while filters are narrow and there's
    // more history. Bounded — we don't fetch forever; once 5 pages are
    // loaded, user has to scroll the sentinel.
    React.useEffect(() => {
        if (!hasNextPage || isFetchingNextPage) return;
        const filtered = type !== "all" || query !== "" || from !== "" || to !== "";
        if (filtered && visible.length < 15 && events.length < 250) {
            const t = setTimeout(() => fetchNextPage(), 120);
            return () => clearTimeout(t);
        }
    }, [
        visible.length,
        events.length,
        hasNextPage,
        isFetchingNextPage,
        type,
        query,
        from,
        to,
        fetchNextPage,
    ]);

    // Sentinel-driven infinite scroll. rootMargin pre-loads ~200px
    // before the bottom so the spinner barely flashes.
    const sentinelRef = React.useRef<HTMLDivElement>(null);
    React.useEffect(() => {
        const el = sentinelRef.current;
        if (!el || !hasNextPage) return;
        const io = new IntersectionObserver(
            (entries) => {
                if (entries[0]?.isIntersecting && !isFetchingNextPage) {
                    fetchNextPage();
                }
            },
            { rootMargin: "200px" },
        );
        io.observe(el);
        return () => io.disconnect();
    }, [hasNextPage, isFetchingNextPage, fetchNextPage]);

    const anyFilter =
        type !== "all" || query !== "" || from !== "" || to !== "";

    function resetFilters() {
        setType("all");
        setQuery("");
        setFrom("");
        setTo("");
    }

    return (
        <div className="space-y-3">
            <CampaignPanel contactId={contactId} />

            <SearchBar value={query} onChange={setQuery} />

            <div className="flex items-center gap-2 flex-wrap">
                <TypeChips value={type} onChange={setType} />
                <DateRange
                    from={from}
                    to={to}
                    setFrom={setFrom}
                    setTo={setTo}
                />
                {anyFilter && (
                    <button
                        type="button"
                        onClick={resetFilters}
                        className="h-6 px-2 rounded text-[11px] font-medium text-slate-500 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                    >
                        Reset
                    </button>
                )}
                <span className="ml-auto text-[10.5px] text-slate-400 tabular-nums">
                    {isLoading
                        ? ""
                        : `${visible.length}${anyFilter ? ` of ${events.length}` : ""}`}
                </span>
            </div>

            {isLoading ? (
                <SkeletonList />
            ) : error ? (
                <div className="rounded-md border border-red-200 bg-red-50/60 px-3 py-2.5 text-[11.5px] text-red-700">
                    Failed to load activity.
                </div>
            ) : visible.length === 0 ? (
                <EmptyState anyFilter={anyFilter} onReset={resetFilters} />
            ) : (
                <div className="rounded-md border border-slate-200 bg-white overflow-hidden">
                    {visible.map((e, i) => (
                        <EventRow
                            key={`${e.type}-${e.at}-${i}`}
                            event={e}
                            highlight={query}
                        />
                    ))}
                </div>
            )}

            <div
                ref={sentinelRef}
                className="h-9 flex items-center justify-center"
            >
                {isFetchingNextPage ? (
                    <span className="inline-flex items-center gap-1.5 text-[11px] text-slate-400">
                        <Loader2Icon className="w-3 h-3 animate-spin" />
                        Loading more
                    </span>
                ) : !hasNextPage && events.length > 0 ? (
                    <span className="text-[10.5px] text-slate-300">
                        End of history
                    </span>
                ) : null}
            </div>
        </div>
    );
}

// ---------------------------------------------------------------------------
// Campaign panel
// ---------------------------------------------------------------------------

function CampaignPanel({ contactId }: { contactId: string }) {
    const { data, isLoading, error } = useContactCampaignStates(contactId);
    const states = data?.data ?? [];

    if (isLoading) {
        return (
            <div className="rounded-md border border-slate-200 bg-white px-3 py-2.5 space-y-1.5">
                <div className="h-2.5 w-1/3 bg-slate-100 rounded animate-pulse" />
                <div className="h-2 w-2/3 bg-slate-100/80 rounded animate-pulse" />
            </div>
        );
    }
    if (error || states.length === 0) return null;

    return (
        <section>
            <h2 className="text-[10px] uppercase tracking-[0.14em] font-semibold text-slate-500 mb-2">
                Campaigns
            </h2>
            <div className="space-y-2">
                {states.map((s) => (
                    <CampaignCard key={s.campaign_id} state={s} />
                ))}
            </div>
        </section>
    );
}

function CampaignCard({ state }: { state: ContactCampaignState }) {
    const [open, setOpen] = React.useState(false);
    const current = state.current_step;

    return (
        <div className="rounded-md border border-slate-200 bg-white overflow-hidden">
            <button
                type="button"
                onClick={() => setOpen((v) => !v)}
                aria-expanded={open}
                className="w-full text-left px-3 py-2.5 hover:bg-slate-50/70 transition-colors"
            >
                <div className="flex items-center gap-2 min-w-0">
                    <MegaphoneIcon className="w-3.5 h-3.5 text-slate-400 shrink-0" />
                    <span className="text-[12.5px] font-medium text-slate-900 truncate">
                        {state.campaign_name}
                    </span>
                    <LeadStatusPill status={state.lead_status} />
                    {state.campaign_status !== "active" && (
                        <span className="text-[10.5px] text-slate-400">
                            {campaignStatusLabel(state.campaign_status)}
                        </span>
                    )}
                    <span className="ml-auto text-[10.5px] text-slate-400 tabular-nums shrink-0">
                        {state.completed_steps}/{state.total_steps} steps
                    </span>
                    <ChevronDownIcon
                        className={`w-3.5 h-3.5 text-slate-400 shrink-0 transition-transform ${open ? "rotate-180" : ""}`}
                    />
                </div>

                <StepRail steps={state.steps} />

                <div className="mt-2 grid grid-cols-1 sm:grid-cols-2 gap-x-3 gap-y-1.5">
                    <PanelFact
                        label="Current step"
                        value={current ? current.label : "Not started"}
                    />
                    <PanelFact
                        label="Last action"
                        value={
                            state.last_action && state.last_action_at ? (
                                <span title={fmtAbsolute(state.last_action_at)}>
                                    {state.last_action} ·{" "}
                                    {fmtRelative(state.last_action_at)}
                                </span>
                            ) : (
                                "Nothing yet"
                            )
                        }
                    />
                    <PanelFact
                        label="Sender"
                        value={
                            state.sender_email ? (
                                <span title={`Every step of this sequence sends from ${state.sender_email}`}>
                                    {state.sender_email}
                                </span>
                            ) : (
                                <span title="Picked when the first email goes out; every follow-up then keeps it.">
                                    Chosen at the first email
                                </span>
                            )
                        }
                    />
                    <div className="sm:col-span-2">
                        <NextActionFact
                            next={state.next}
                            endedReason={state.ended_reason}
                            failureReason={state.failure_reason}
                        />
                    </div>
                </div>
            </button>

            <AnimatePresence initial={false}>
                {open && (
                    <motion.div
                        key="steps"
                        initial={{ height: 0, opacity: 0 }}
                        animate={{ height: "auto", opacity: 1 }}
                        exit={{ height: 0, opacity: 0 }}
                        transition={{ duration: 0.18, ease: [0.32, 0.72, 0, 1] }}
                        className="overflow-hidden"
                    >
                        <div className="border-t border-slate-100">
                            {state.steps.map((s) => (
                                <StepRow
                                    key={s.id}
                                    step={s}
                                    isNext={!!state.next?.step_id && state.next.step_id === s.id}
                                />
                            ))}
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}

function PanelFact({
    label,
    value,
}: {
    label: string;
    value: React.ReactNode;
}) {
    return (
        <div className="min-w-0">
            <div className="text-[10px] uppercase tracking-[0.12em] text-slate-400 font-medium">
                {label}
            </div>
            <div className="text-[11.5px] text-slate-700 truncate">{value}</div>
        </div>
    );
}

// The next action, worded by how firm its timing is: a due step carries the
// scheduler's slot, a waiting one carries the constraint and an earliest
// time, a paused or blocked one only the reason. Never a made-up timestamp.
function NextActionFact({
    next,
    endedReason,
    failureReason,
}: {
    next?: ContactNextAction | null;
    endedReason?: string;
    failureReason?: string;
}) {
    if (!next) {
        return (
            <PanelFact
                label="Next"
                value={
                    <span className="text-slate-500">
                        {endedReason || "Nothing scheduled"}
                        {failureReason ? ` · ${failureReason}` : ""}
                    </span>
                }
            />
        );
    }
    const Icon =
        next.state === "due"
            ? ClockIcon
            : next.state === "paused"
              ? PauseIcon
              : next.state === "blocked"
                ? OctagonXIcon
                : ClockIcon;
    const tone =
        next.state === "due"
            ? "bg-sky-50 text-sky-700"
            : next.state === "blocked"
              ? "bg-red-50 text-red-700"
              : next.state === "paused"
                ? "bg-slate-100 text-slate-600"
                : "bg-amber-50 text-amber-700";
    const stateLabel =
        next.state === "due"
            ? "Due"
            : next.state === "waiting"
              ? "Waiting"
              : next.state === "paused"
                ? "Paused"
                : "Blocked";

    return (
        <div className="min-w-0">
            <div className="text-[10px] uppercase tracking-[0.12em] text-slate-400 font-medium">
                Next
            </div>
            <div className="text-[11.5px] text-slate-700 flex items-center gap-1.5 flex-wrap">
                <span className="font-medium text-slate-900">{next.step_label}</span>
                {next.subject && (
                    <span className="text-slate-500 truncate">· {next.subject}</span>
                )}
                <span
                    className={`inline-flex items-center gap-1 h-4 px-1.5 rounded text-[10.5px] font-medium ${tone}`}
                >
                    <Icon className="w-3 h-3" />
                    {stateLabel}
                </span>
                {next.state === "due" && next.scheduled_at && (
                    <span title="When this campaign next works through its queue. Leads queued ahead can still push this step to a later pass.">
                        next pass {fmtAbsolute(next.scheduled_at)}
                    </span>
                )}
                {next.state !== "due" && next.not_before && (
                    <span title={fmtAbsolute(next.not_before)}>
                        not before {fmtAbsolute(next.not_before)}
                    </span>
                )}
            </div>
            {next.constraint && (
                <div className="text-[11px] text-slate-500 mt-0.5">{next.constraint}</div>
            )}
        </div>
    );
}

// One dot per step: filled when sent, amber while in flight, red on a
// bounce or exhausted failure, hollow otherwise.
function StepRail({ steps }: { steps: ContactCampaignStep[] }) {
    if (steps.length === 0) return null;
    return (
        <div className="mt-2 flex items-center gap-1">
            {steps.map((s, i) => {
                const st = stepState(s);
                const dot =
                    st === "sent"
                        ? "bg-sky-600 border-sky-600"
                        : st === "in_flight"
                          ? "bg-amber-400 border-amber-400"
                          : st === "failed"
                            ? "bg-red-500 border-red-500"
                            : "bg-white border-slate-300";
                return (
                    <React.Fragment key={s.id}>
                        {i > 0 && (
                            <span
                                className={`h-px flex-1 min-w-2 ${st === "sent" || st === "in_flight" ? "bg-sky-300" : "bg-slate-200"}`}
                            />
                        )}
                        <span
                            title={`${s.label}${s.sent_at ? ` · sent ${fmtAbsolute(s.sent_at)}` : ""}`}
                            className={`w-2.5 h-2.5 rounded-full border shrink-0 ${dot}`}
                        />
                    </React.Fragment>
                );
            })}
        </div>
    );
}

type StepState = "sent" | "in_flight" | "failed" | "pending";

function stepState(s: ContactCampaignStep): StepState {
    if (s.sent_at) return "sent";
    if (s.in_flight) return "in_flight";
    if (s.failed_at) return "failed";
    return "pending";
}

function StepRow({ step, isNext }: { step: ContactCampaignStep; isNext: boolean }) {
    const st = stepState(step);
    const facts: string[] = [];
    if (step.sent_at) facts.push(`sent ${fmtRelative(step.sent_at)}`);
    if (step.opened_at) facts.push("opened");
    if (step.clicked_at) facts.push("clicked");
    if (step.replied_at) facts.push("replied");
    if (step.bounced_at) facts.push("bounced");
    if (!step.sent_at && step.failed_at)
        facts.push(`failed ${step.attempts ?? 0}x`);
    if (st === "in_flight") facts.push("sending");
    if (isNext) facts.push("next");

    return (
        <div className="px-3 py-1.5 border-b last:border-b-0 border-slate-100 flex items-center gap-2 text-[11.5px]">
            <span
                className={`w-4 h-4 rounded-full inline-flex items-center justify-center shrink-0 ${
                    st === "sent"
                        ? "bg-sky-600 text-white"
                        : st === "failed"
                          ? "bg-red-500 text-white"
                          : st === "in_flight"
                            ? "bg-amber-400 text-white"
                            : "border border-slate-300 text-transparent"
                }`}
            >
                <CheckIcon className="w-2.5 h-2.5" />
            </span>
            <span
                className={`truncate ${st === "pending" && !isNext ? "text-slate-500" : "text-slate-900"}`}
            >
                {step.label}
                {step.subject && (
                    <span className="text-slate-400"> · {step.subject}</span>
                )}
            </span>
            <span className="ml-auto text-[10.5px] text-slate-400 shrink-0">
                {facts.join(" · ")}
            </span>
        </div>
    );
}

function LeadStatusPill({ status }: { status: LeadStatus }) {
    const map: Record<LeadStatus, { label: string; cls: string }> = {
        pending: { label: "Queued", cls: "bg-slate-100 text-slate-600" },
        active: { label: "Processing", cls: "bg-sky-50 text-sky-700" },
        completed: { label: "Done", cls: "bg-emerald-50 text-emerald-700" },
        replied: { label: "Replied", cls: "bg-emerald-50 text-emerald-700" },
        bounced: { label: "Bounced", cls: "bg-red-50 text-red-700" },
        failed: { label: "Failed", cls: "bg-red-50 text-red-700" },
        unsubscribed: { label: "Unsubscribed", cls: "bg-slate-100 text-slate-600" },
        undeliverable: { label: "Undeliverable", cls: "bg-amber-50 text-amber-700" },
    };
    const m = map[status] ?? { label: status, cls: "bg-slate-100 text-slate-600" };
    return (
        <span
            className={`inline-flex h-4 items-center px-1.5 rounded text-[10.5px] font-medium shrink-0 ${m.cls}`}
        >
            {m.label}
        </span>
    );
}

function campaignStatusLabel(status: string): string {
    switch (status) {
        case "paused":
            return "paused";
        case "paused_guardrail":
            return "auto-paused";
        case "paused_undeliverable":
            return "paused, needs verification";
        case "paused_no_accounts":
            return "paused, no accounts";
        case "paused_trial_expired":
            return "paused, trial expired";
        case "completed":
            return "finished";
        case "draft":
            return "not started";
        default:
            return status.replace(/_/g, " ");
    }
}

// ---------------------------------------------------------------------------
// Filters
// ---------------------------------------------------------------------------

function applyFilters(
    events: ContactTimelineEvent[],
    f: { type: FilterId; query: string; from: string; to: string },
): ContactTimelineEvent[] {
    const ql = f.query.trim().toLowerCase();
    const fromMs = f.from ? new Date(f.from + "T00:00:00").getTime() : -Infinity;
    const toMs = f.to ? new Date(f.to + "T23:59:59.999").getTime() : Infinity;
    return events.filter((e) => {
        const tMs = new Date(e.at).getTime();
        if (tMs < fromMs || tMs > toMs) return false;
        switch (f.type) {
            case "emails":
                if (!EMAIL_TYPES.includes(e.type)) return false;
                break;
            case "replies":
                if (e.type !== "email_replied" && e.type !== "reply_received")
                    return false;
                break;
            case "deliv":
                if (e.type !== "deliverability" && e.type !== "suppressed")
                    return false;
                break;
            case "notes":
                if (e.type !== "note") return false;
                break;
            case "meetings":
                if (!MEETING_TYPES.includes(e.type)) return false;
                break;
            case "campaigns":
                if (!CAMPAIGN_TYPES.includes(e.type)) return false;
                break;
            case "lifecycle":
                if (!LIFECYCLE_TYPES.includes(e.type)) return false;
                break;
            case "website":
                if (e.type !== "page_hit") return false;
                break;
            case "all":
                break;
        }
        if (ql) {
            const hay = [
                e.subject,
                e.content,
                e.campaign_name,
                e.step_name,
                e.email_account_email,
                e.email_account_name,
                e.category_title,
                e.source,
                e.source_detail,
                e.reason,
                e.intent,
                e.page_hit?.url,
                e.page_hit?.referrer_domain,
                e.page_hit?.utm_source,
                e.page_hit?.utm_campaign,
                e.link?.url,
                e.link?.label,
                e.link?.utm_content,
                e.link?.utm_campaign,
                e.origin?.client,
                e.origin?.city,
                e.origin?.country_code,
            ]
                .filter(Boolean)
                .join(" ")
                .toLowerCase();
            if (!hay.includes(ql)) return false;
        }
        return true;
    });
}

function SearchBar({
    value,
    onChange,
}: {
    value: string;
    onChange: (v: string) => void;
}) {
    return (
        <div className="relative">
            <SearchIcon className="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-slate-400 pointer-events-none" />
            <input
                type="text"
                value={value}
                onChange={(e) => onChange(e.target.value)}
                placeholder="Search subject, content, campaign…"
                className="w-full h-8 pl-8 pr-7 rounded-md border border-slate-200 bg-white text-[12px] text-slate-900 placeholder:text-slate-400 focus:border-slate-400 outline-none transition-colors"
            />
            {value && (
                <button
                    type="button"
                    onClick={() => onChange("")}
                    aria-label="Clear search"
                    className="absolute right-1.5 top-1/2 -translate-y-1/2 size-5 rounded text-slate-400 hover:text-slate-700 hover:bg-slate-100 inline-flex items-center justify-center"
                >
                    <XIcon className="w-3 h-3" />
                </button>
            )}
        </div>
    );
}

function TypeChips({
    value,
    onChange,
}: {
    value: FilterId;
    onChange: (v: FilterId) => void;
}) {
    return (
        <div className="inline-flex flex-wrap bg-slate-100 rounded-md p-0.5">
            {FILTERS.map((f) => {
                const isActive = value === f.id;
                return (
                    <button
                        key={f.id}
                        type="button"
                        onClick={() => onChange(f.id)}
                        className="relative h-6 px-2 rounded text-[11px] font-medium outline-none whitespace-nowrap"
                    >
                        {isActive && (
                            <motion.div
                                layoutId="contact-activity-type"
                                className="absolute inset-0 rounded bg-white shadow-sm"
                                transition={{
                                    type: "spring",
                                    duration: 0.3,
                                    bounce: 0.15,
                                }}
                            />
                        )}
                        <span
                            className={`relative z-10 transition-colors ${
                                isActive
                                    ? "text-slate-900"
                                    : "text-slate-500 hover:text-slate-800"
                            }`}
                        >
                            {f.label}
                        </span>
                    </button>
                );
            })}
        </div>
    );
}

function DateRange({
    from,
    to,
    setFrom,
    setTo,
}: {
    from: string;
    to: string;
    setFrom: (v: string) => void;
    setTo: (v: string) => void;
}) {
    const [open, setOpen] = React.useState(false);
    const ref = React.useRef<HTMLDivElement>(null);
    useClickOutside(ref, () => setOpen(false));
    // The trigger wraps anywhere along the toolbar row, so the panel side is
    // measured, not fixed: a fixed right-0 clipped it against the drawer edge.
    const align = useFlipAlignment(ref, open, 256);

    const active = !!from || !!to;
    const label = active
        ? `${fmtChip(from) || "…"} → ${fmtChip(to) || "today"}`
        : "Any date";

    function setPreset(days: number) {
        const end = new Date();
        const start = new Date();
        start.setDate(start.getDate() - days);
        setFrom(toInput(start));
        setTo(toInput(end));
    }

    return (
        <div ref={ref} className="relative">
            <button
                type="button"
                onClick={() => setOpen((v) => !v)}
                className={`h-6 px-2 rounded text-[11px] font-medium inline-flex items-center gap-1 transition-colors ${
                    active
                        ? "bg-slate-900 text-white hover:bg-slate-800"
                        : "bg-slate-100 text-slate-600 hover:text-slate-900"
                }`}
            >
                <CalendarIcon className="w-3 h-3" />
                {label}
            </button>
            {open && (
                <div
                    className={`absolute ${align === "right" ? "right-0" : "left-0"} top-7 z-50 w-64 max-w-[min(256px,calc(100vw-2rem))] p-2.5 rounded-md border border-slate-200 bg-white shadow-lg`}
                >
                    <div className="grid grid-cols-2 gap-2">
                        <div>
                            <label className="block text-[10px] uppercase tracking-[0.12em] font-medium text-slate-500 mb-1">
                                From
                            </label>
                            <DatePicker
                                value={from}
                                onChange={setFrom}
                                placeholder="From"
                                clearable={false}
                                className="w-full"
                            />
                        </div>
                        <div>
                            <label className="block text-[10px] uppercase tracking-[0.12em] font-medium text-slate-500 mb-1">
                                To
                            </label>
                            <DatePicker
                                value={to}
                                onChange={setTo}
                                placeholder="To"
                                clearable={false}
                                className="w-full"
                            />
                        </div>
                    </div>
                    <div className="flex flex-wrap gap-1 mt-2.5">
                        <Preset onClick={() => setPreset(7)}>Last 7d</Preset>
                        <Preset onClick={() => setPreset(30)}>Last 30d</Preset>
                        <Preset onClick={() => setPreset(90)}>Last 90d</Preset>
                        <button
                            type="button"
                            onClick={() => {
                                setFrom("");
                                setTo("");
                            }}
                            className="h-6 px-2 ml-auto rounded text-[10.5px] font-medium text-slate-500 hover:text-slate-900 hover:bg-slate-100"
                        >
                            Clear
                        </button>
                    </div>
                </div>
            )}
        </div>
    );
}

function Preset({
    onClick,
    children,
}: {
    onClick: () => void;
    children: React.ReactNode;
}) {
    return (
        <button
            type="button"
            onClick={onClick}
            className="h-6 px-2 rounded text-[10.5px] font-medium border border-slate-200 text-slate-600 hover:text-slate-900 hover:bg-slate-50 transition-colors"
        >
            {children}
        </button>
    );
}

function EmptyState({
    anyFilter,
    onReset,
}: {
    anyFilter: boolean;
    onReset: () => void;
}) {
    return (
        <div className="rounded-md border border-dashed border-slate-200 px-3 py-10 text-center">
            <div className="text-[11.5px] text-slate-500">
                {anyFilter ? "No events match these filters." : "No activity yet."}
            </div>
            {anyFilter && (
                <button
                    type="button"
                    onClick={onReset}
                    className="mt-2 h-6 px-2 rounded text-[11px] font-medium text-slate-600 hover:text-slate-900 hover:bg-slate-100"
                >
                    Reset filters
                </button>
            )}
        </div>
    );
}

function SkeletonList() {
    return (
        <div className="rounded-md border border-slate-200 bg-white overflow-hidden">
            {Array.from({ length: 6 }).map((_, i) => (
                <div
                    key={i}
                    className="px-3 py-2.5 border-b last:border-b-0 border-slate-100 flex items-start gap-2.5"
                    style={{ animationDelay: `${i * 60}ms` }}
                >
                    <div className="w-3.5 h-3.5 rounded bg-slate-100 mt-0.5 shrink-0 animate-pulse" />
                    <div className="flex-1 space-y-1.5">
                        <div
                            className="h-2.5 bg-slate-100 rounded animate-pulse"
                            style={{ width: `${40 + ((i * 13) % 40)}%` }}
                        />
                        <div
                            className="h-2 bg-slate-100/80 rounded animate-pulse"
                            style={{ width: `${30 + ((i * 11) % 50)}%` }}
                        />
                    </div>
                    <div className="h-2 w-10 bg-slate-100 rounded animate-pulse mt-1 shrink-0" />
                </div>
            ))}
        </div>
    );
}

// ---------------------------------------------------------------------------
// Feed rows
// ---------------------------------------------------------------------------

// Collapsed: icon, label, subject, a one-line meta. Expanded (click): every
// field the event carries, as a label/value grid.
function EventRow({
    event,
    highlight,
}: {
    event: ContactTimelineEvent;
    highlight: string;
}) {
    const [open, setOpen] = React.useState(false);
    const { Icon, label } = visualFor(event);
    const details = detailsFor(event);

    return (
        <div className="border-b last:border-b-0 border-slate-100">
            <button
                type="button"
                onClick={() => setOpen((v) => !v)}
                aria-expanded={open}
                className="w-full text-left px-3 py-2 hover:bg-slate-50/70 transition-colors"
            >
                <div className="flex items-start gap-2.5">
                    <Icon className="w-3.5 h-3.5 text-slate-400 mt-0.5 shrink-0" />
                    <div className="min-w-0 flex-1">
                        <div className="flex items-baseline gap-1.5 min-w-0">
                            <span className="text-[12px] font-medium text-slate-900 shrink-0">
                                {label}
                            </span>
                            {event.link && (
                                <span className="text-[11.5px] text-slate-700 truncate">
                                    <Highlight text={event.link.label || linkHost(event.link.url)} q={highlight} />
                                </span>
                            )}
                            {event.machine && <MachineBadge reason={event.machine_reason} />}
                            {event.subject && (
                                <span className="text-[11.5px] text-slate-600 truncate">
                                    · <Highlight text={event.subject} q={highlight} />
                                </span>
                            )}
                        </div>
                        <EventMeta event={event} highlight={highlight} />
                    </div>
                    <span
                        className="text-[10.5px] text-slate-400 tabular-nums shrink-0 mt-0.5"
                        title={fmtAbsolute(event.at)}
                    >
                        {fmtRelative(event.at)}
                    </span>
                    <ChevronDownIcon
                        className={`w-3.5 h-3.5 text-slate-300 shrink-0 mt-0.5 transition-transform ${open ? "rotate-180" : ""}`}
                    />
                </div>
                {event.content && !open && (
                    <div className="text-[11.5px] text-slate-700 mt-1.5 ml-6 whitespace-pre-wrap break-words border-l-2 border-slate-100 pl-2 line-clamp-3">
                        <Highlight text={event.content} q={highlight} />
                    </div>
                )}
            </button>
            <AnimatePresence initial={false}>
                {open && (
                    <motion.div
                        key="details"
                        initial={{ height: 0, opacity: 0 }}
                        animate={{ height: "auto", opacity: 1 }}
                        exit={{ height: 0, opacity: 0 }}
                        transition={{ duration: 0.18, ease: [0.32, 0.72, 0, 1] }}
                        className="overflow-hidden"
                    >
                        <div className="mx-3 mb-2.5 ml-9 rounded-md border border-slate-200 bg-slate-50/60 px-2.5 py-2 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-[11px]">
                            {details.map(([k, v]) => (
                                <React.Fragment key={k}>
                                    <span className="text-slate-400 uppercase tracking-[0.1em] text-[10px] font-medium pt-px">
                                        {k}
                                    </span>
                                    {/* wrap-anywhere: break-words leaves grid min-content wide, so long URLs overflowed the card */}
                                    <span className="text-slate-700 min-w-0 wrap-anywhere whitespace-pre-wrap">
                                        {typeof v === "string" ? (
                                            <Highlight text={v} q={highlight} />
                                        ) : (
                                            v
                                        )}
                                    </span>
                                </React.Fragment>
                            ))}
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}

// Every detail an event carries, in the order a reader wants them.
function detailsFor(e: ContactTimelineEvent): [string, React.ReactNode][] {
    const out: [string, React.ReactNode][] = [];
    const add = (k: string, v?: React.ReactNode | null) => {
        if (v !== undefined && v !== null && v !== "") out.push([k, v]);
    };
    add("When", fmtAbsolute(e.at));
    if (e.type === "contact_created") {
        add("Source", sourceLabel(e.source));
        add("Detail", e.source_detail);
    }
    add("Campaign", e.campaign_name);
    add("Step", e.step_name);
    add("Subject", e.subject);
    if (e.email_account_email) {
        add(
            "Mailbox",
            e.email_account_name
                ? `${e.email_account_name} <${e.email_account_email}>`
                : e.email_account_email,
        );
    }
    add("Category", e.category_title);
    add("Intent", e.intent);
    if (e.type === "deliverability" || e.type === "suppressed") {
        add("Type", e.source);
        add("Provider", e.provider && e.provider !== "manual" ? e.provider : null);
    }
    if (e.type.startsWith("meeting_")) {
        add("Scheduled for", e.scheduled_for ? fmtAbsolute(e.scheduled_for) : null);
        add("Calendar", providerLabel(e.source));
        add("State", e.meeting_state);
        const joinUrl = safeHttpUrl(e.join_url);
        if (joinUrl && e.type !== "meeting_canceled") {
            add(
                "Join",
                <a
                    href={joinUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="text-sky-600 hover:text-sky-700 font-medium"
                >
                    {joinUrl}
                </a>,
            );
        }
    }
    if (e.type === "page_hit" && e.page_hit) {
        const h = e.page_hit;
        const link = (u: string) => (
            <a
                href={u}
                target="_blank"
                rel="noopener noreferrer"
                className="text-sky-600 hover:text-sky-700 break-all"
            >
                {u}
            </a>
        );
        add("Page URL", safeHttpUrl(h.url) ? link(h.url) : h.url);
        add("Page title", h.title);
        add("Referrer", safeHttpUrl(h.referrer) ? link(h.referrer) : h.referrer);
        add("Device", cap(h.device_type));
        add("Operating system", h.os);
        add("Browser", [h.browser, h.browser_version].filter(Boolean).join(" "));
        add("Device brand", h.device_brand);
        add("Language", h.language);
        add("Timezone", h.timezone);
        add("Screen", h.screen_width && h.screen_height ? `${h.screen_width} × ${h.screen_height}` : null);
        add("Location", [h.city, h.region, h.country_code].filter(Boolean).join(", "));
        add("UTM source", h.utm_source);
        add("UTM medium", h.utm_medium);
        add("UTM campaign", h.utm_campaign);
        add("UTM term", h.utm_term);
        add("UTM content", h.utm_content);
        add("Session", <span className="font-mono">{h.session_key.slice(0, 8)}</span>);
    }
    if (e.link) {
        const l = e.link;
        add("Link text", l.label);
        add(
            "Link URL",
            safeHttpUrl(l.url) ? (
                <a
                    href={l.url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="text-sky-600 hover:text-sky-700 break-all"
                >
                    {l.url}
                </a>
            ) : (
                l.url
            ),
        );
        add("UTM source", l.utm_source);
        add("UTM medium", l.utm_medium);
        add("UTM campaign", l.utm_campaign);
        add("UTM term", l.utm_term);
        add("UTM content", l.utm_content);
        add("Browser", l.user_agent);
    }
    if (e.origin) {
        const o = e.origin;
        add("Client", o.client);
        add("Device", cap(o.device_type ?? ""));
        add("Operating system", o.os);
        add("Browser", [o.browser, o.browser_version].filter(Boolean).join(" "));
        add("Location", [o.city, o.region, o.country_code].filter(Boolean).join(", "));
    }
    if (e.type === "email_opened" || e.type === "email_clicked") {
        add("Classified as", e.machine ? machineLabel(e.machine_reason) : "A person");
    }
    add("Reason", e.reason);
    add("Content", e.content);
    if (e.task_id) add("Task", <span className="font-mono">{e.task_id}</span>);
    return out;
}

// What the classifier caught, in words a reader can act on.
function machineLabel(reason?: string | null): string {
    switch (reason) {
        case "instant":
            return "Automated: fetched within seconds of sending, before anyone could have read it";
        case "burst":
            return "Automated: several links followed within seconds, the way a security scanner walks an email";
        case "prefetch":
            return "Automated: fetched by a mail proxy or a client with no browser";
        case "scanner":
            return "Automated: came from a mail security network, not the recipient's own device";
        default:
            return "Automated: a mail privacy proxy or scanner, not a person";
    }
}

function linkHost(url: string): string {
    try {
        return new URL(url).host;
    } catch {
        return url;
    }
}

function MachineBadge({ reason }: { reason?: string | null }) {
    return (
        <span
            className="inline-flex items-center rounded-sm bg-amber-50 text-amber-700 border border-amber-200 px-1 text-[9.5px] font-medium uppercase tracking-[0.1em] shrink-0"
            title={machineLabel(reason)}
        >
            auto
        </span>
    );
}

function cap(s: string): string {
    if (!s || s === "unknown") return "";
    return s.charAt(0).toUpperCase() + s.slice(1);
}

// "Gmail", or "Chrome on Windows", or "Mobile" when the user agent said
// little; empty when it said nothing.
function originLabel(o: EngagementOrigin): string {
    if (o.client) return o.client;
    const browser = [o.browser, o.browser_version ? o.browser_version.split(".")[0] : ""].filter(Boolean).join(" ");
    if (browser && o.os) return `${browser} on ${o.os}`;
    if (browser) return browser;
    if (o.os) return o.os;
    return cap(o.device_type ?? "");
}

function EventMeta({
    event,
    highlight,
}: {
    event: ContactTimelineEvent;
    highlight: string;
}) {
    // Page views: path, where from, what device, which UTM source.
    if (event.type === "page_hit" && event.page_hit) {
        const h = event.page_hit;
        const device = [cap(h.device_type), h.os, h.browser].filter(Boolean).join(" / ");
        const bits: React.ReactNode[] = [
            <span key="path" className="font-mono truncate max-w-[260px]">
                <Highlight text={h.path} q={highlight} />
            </span>,
        ];
        if (h.referrer_domain) {
            bits.push(
                <span key="ref">
                    from <Highlight text={h.referrer_domain} q={highlight} />
                </span>,
            );
        }
        if (device) bits.push(<span key="device">{device}</span>);
        if (h.utm_source) {
            bits.push(
                <span key="utm">
                    utm <Highlight text={h.utm_source} q={highlight} />
                </span>,
            );
        }
        return (
            <div className="text-[11px] text-slate-500 mt-0.5 flex gap-1.5 flex-wrap items-center">
                {bits.map((b, i) => (
                    <React.Fragment key={i}>
                        {b}
                        {i < bits.length - 1 && <span className="text-slate-300">·</span>}
                    </React.Fragment>
                ))}
            </div>
        );
    }

    // Meetings get a dedicated meta line: when the call is set for, which
    // calendar it came from, and a one-click join link (when not canceled).
    if (event.type.startsWith("meeting_")) {
        const when = event.scheduled_for
            ? new Date(event.scheduled_for).toLocaleString(undefined, {
                  month: "short",
                  day: "numeric",
                  hour: "numeric",
                  minute: "2-digit",
              })
            : null;
        const provider = providerLabel(event.source);
        const joinUrl = safeHttpUrl(event.join_url);
        return (
            <div className="text-[11px] text-slate-500 mt-0.5 flex gap-1.5 flex-wrap items-center">
                {when && <span>for {when}</span>}
                {when && provider && <span className="text-slate-300">·</span>}
                {provider && <span>via {provider}</span>}
                {event.reason && (
                    <>
                        <span className="text-slate-300">·</span>
                        <span className="text-slate-700">{event.reason}</span>
                    </>
                )}
                {event.type !== "meeting_canceled" && joinUrl && (
                    <>
                        <span className="text-slate-300">·</span>
                        <a
                            href={joinUrl}
                            target="_blank"
                            rel="noopener noreferrer"
                            onClick={(e) => e.stopPropagation()}
                            className="text-sky-600 hover:text-sky-700 font-medium"
                        >
                            Join
                        </a>
                    </>
                )}
            </div>
        );
    }

    const parts: React.ReactNode[] = [];

    // Lifecycle rows name the thing that changed, not the sending context.
    if (event.type === "contact_created") {
        if (event.source_detail) {
            parts.push(
                <span key="detail">
                    <Highlight text={event.source_detail} q={highlight} />
                </span>,
            );
        }
    } else if (CAMPAIGN_TYPES.includes(event.type)) {
        if (event.campaign_name) {
            parts.push(
                <span key="campaign">
                    <Highlight text={event.campaign_name} q={highlight} />
                </span>,
            );
        }
    } else if (event.type === "form_submitted") {
        if (event.form_name) {
            parts.push(
                <span key="form" className="inline-flex items-center gap-1">
                    <ClipboardListIcon className="w-3 h-3 text-slate-400" />
                    <Highlight text={event.form_name} q={highlight} />
                </span>,
            );
        }
    } else if (event.type === "category_added" || event.type === "category_removed") {
        if (event.category_title) {
            parts.push(
                <span key="category" className="inline-flex items-center gap-1">
                    <TagIcon className="w-3 h-3 text-slate-400" />
                    <Highlight text={event.category_title} q={highlight} />
                </span>,
            );
        }
    } else {
        if (event.email_account_email) {
            parts.push(
                <span key="mailbox" className="font-mono">
                    from{" "}
                    <Highlight text={event.email_account_email} q={highlight} />
                </span>,
            );
        }
        if (event.campaign_name) {
            parts.push(
                <span key="campaign">
                    in <Highlight text={event.campaign_name} q={highlight} />
                </span>,
            );
        }
        if (event.step_name) {
            parts.push(
                <span key="sequence">
                    step <Highlight text={event.step_name} q={highlight} />
                </span>,
            );
        }
        if (event.link?.utm_content) {
            parts.push(
                <span key="utm">
                    utm <Highlight text={event.link.utm_content} q={highlight} />
                </span>,
            );
        }
        if (event.origin) {
            const on = originLabel(event.origin);
            if (on) parts.push(<span key="origin">{on}</span>);
            const where = [event.origin.city, event.origin.country_code].filter(Boolean).join(", ");
            if (where) {
                parts.push(
                    <span key="where">
                        <Highlight text={where} q={highlight} />
                    </span>,
                );
            }
        }
        if (event.intent) {
            parts.push(<span key="intent">intent: {event.intent}</span>);
        }
        if (event.provider && event.provider !== "manual") {
            parts.push(<span key="provider">via {event.provider}</span>);
        }
        if (event.source) {
            parts.push(<span key="source">type: {event.source}</span>);
        }
        if (event.reason) {
            parts.push(
                <span key="reason" className="text-slate-700">
                    <Highlight text={event.reason} q={highlight} />
                </span>,
            );
        }
    }

    if (parts.length === 0) return null;

    return (
        <div className="text-[11px] text-slate-500 mt-0.5 flex gap-1.5 flex-wrap">
            {parts.map((p, i) => (
                <React.Fragment key={i}>
                    {p}
                    {i < parts.length - 1 && (
                        <span className="text-slate-300">·</span>
                    )}
                </React.Fragment>
            ))}
        </div>
    );
}

function Highlight({ text, q }: { text: string; q: string }) {
    const ql = q.trim().toLowerCase();
    if (!ql) return <>{text}</>;
    const lower = text.toLowerCase();
    const idx = lower.indexOf(ql);
    if (idx < 0) return <>{text}</>;
    return (
        <>
            {text.slice(0, idx)}
            <mark className="bg-amber-100 text-amber-900 rounded-sm px-0.5">
                {text.slice(idx, idx + ql.length)}
            </mark>
            {text.slice(idx + ql.length)}
        </>
    );
}

// Meeting join links come from a scheduling provider's webhook and are stored
// verbatim, so only http(s) may become an href; anything else is not linked.
function safeHttpUrl(raw?: string | null): string | null {
    if (!raw) return null;
    try {
        const u = new URL(raw);
        return u.protocol === "https:" || u.protocol === "http:" ? u.href : null;
    } catch {
        return null;
    }
}

function providerLabel(source?: string | null): string | null {
    if (!source) return null;
    if (source === "cal_com") return "Cal.com";
    if (source === "calendly") return "Calendly";
    return source;
}

// The first-touch source as a person would say it.
export function sourceLabel(source?: string | null): string {
    switch (source) {
        case "manual":
            return "Added manually";
        case "campaign":
            return "Added from a campaign";
        case "import":
            return "Imported from a file";
        case "sheet_sync":
            return "Synced from Google Sheets";
        case "api":
            return "Created via the API";
        case "ai_assistant":
            return "Created by the AI assistant";
        case "form":
            return "Submitted a form";
        case "automation":
            return "Created by an automation";
        case "unknown":
        case undefined:
        case null:
            return "Unknown";
        default:
            return source;
    }
}

function visualFor(e: ContactTimelineEvent): {
    Icon: typeof MailIcon;
    label: string;
} {
    switch (e.type) {
        case "email_sent":
            return { Icon: MailIcon, label: "Email sent" };
        case "email_opened":
            return { Icon: MailOpenIcon, label: "Opened" };
        case "email_clicked":
            return { Icon: MousePointerClickIcon, label: e.link ? "Clicked" : "Clicked link" };
        case "email_replied":
            return { Icon: ReplyIcon, label: "Replied" };
        case "reply_received":
            return { Icon: MessageSquareIcon, label: "Reply received" };
        case "email_bounced":
            return { Icon: MailWarningIcon, label: "Bounced" };
        case "deliverability":
            return { Icon: AlertOctagonIcon, label: "Deliverability event" };
        case "suppressed":
            return { Icon: BanIcon, label: "Suppressed" };
        case "note":
            return { Icon: StickyNoteIcon, label: "Note added" };
        case "meeting_booked":
            return { Icon: CalendarPlusIcon, label: "Meeting booked" };
        case "meeting_rescheduled":
            return { Icon: CalendarClockIcon, label: "Meeting rescheduled" };
        case "meeting_canceled":
            return { Icon: CalendarXIcon, label: "Meeting canceled" };
        case "contact_created":
            return { Icon: UserPlusIcon, label: createdLabel(e.source) };
        case "campaign_added":
            return { Icon: MegaphoneIcon, label: "Added to campaign" };
        case "campaign_removed":
            return { Icon: MegaphoneIcon, label: "Removed from campaign" };
        case "category_added":
            return { Icon: TagIcon, label: "Added to category" };
        case "category_removed":
            return { Icon: TagIcon, label: "Removed from category" };
        case "form_submitted":
            return { Icon: ClipboardListIcon, label: "Submitted a form" };
        case "page_hit":
            return { Icon: GlobeIcon, label: e.page_hit?.landing ? "Landed on" : "Page hit" };
        default:
            return { Icon: MailIcon, label: e.type };
    }
}

function createdLabel(source?: string | null): string {
    switch (source) {
        case "import":
            return "Imported";
        case "sheet_sync":
            return "Synced from sheet";
        case "api":
            return "Created via API";
        case "campaign":
            return "Added from campaign";
        case "ai_assistant":
            return "Created by AI assistant";
        case "form":
            return "Submitted a form";
        case "automation":
            return "Created by automation";
        case "manual":
            return "Created manually";
        default:
            return "Contact created";
    }
}

function fmtChip(d: string): string {
    if (!d) return "";
    const dt = new Date(d + "T00:00:00");
    if (Number.isNaN(dt.getTime())) return d;
    return dt.toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

function toInput(d: Date): string {
    const y = d.getFullYear();
    const m = String(d.getMonth() + 1).padStart(2, "0");
    const day = String(d.getDate()).padStart(2, "0");
    return `${y}-${m}-${day}`;
}
