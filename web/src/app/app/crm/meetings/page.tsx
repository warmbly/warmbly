// Meetings / Calls — booked calls as a first-class CRM surface.
//
// Two sources feed this page:
//   1. Manual — meetings the user schedules/logs here with "New meeting"
//      (source "manual"); created instantly in Warmbly, no external site.
//   2. Auto — calls a prospect self-books through a connected Calendly / Cal.com
//      link, captured over that provider's inbound webhook.
//
// Either way they land here + on the contact timeline, live (realtime meeting
// events invalidate this list + summary). Server-driven for scale: timeframe /
// status / text filters + offset infinite scroll; header totals are COUNTs over
// the whole set via /meetings/summary, not a reduce over the loaded page.

import React from "react";
import {
    CalendarClockIcon,
    CalendarPlusIcon,
    CableIcon,
    Loader2Icon,
    PlusIcon,
    Trash2Icon,
    VideoIcon,
    XCircleIcon,
    RotateCcwIcon,
} from "lucide-react";
import toast from "react-hot-toast";
import {
    Page,
    PageBody,
    PageTopbar,
    SectionBar,
    Stat,
    StatStrip,
    TopbarAction,
    EmptyBlock,
} from "@/components/layout/Page";
import { SearchInput } from "@/components/ui/field";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuTrigger,
} from "@/components/ui/popover-menu";
import AnimatedNumber from "@/components/ui/AnimatedNumber";
import NewMeetingDialog from "@/components/app/meetings/NewMeetingDialog";
import { useConfirm } from "@/hooks/context/confirm";
import useSearchMeetings from "@/lib/api/hooks/app/meetings/useSearchMeetings";
import useMeetingsSummary from "@/lib/api/hooks/app/meetings/useMeetingsSummary";
import useDeleteMeeting from "@/lib/api/hooks/app/meetings/useDeleteMeeting";
import {
    PROVIDER_LABELS,
    type MeetingBooking,
    type MeetingStatus,
    type MeetingsSearch,
} from "@/lib/api/models/app/integrations/Integration";
import { cn } from "@/lib/utils";

type Timeframe = "upcoming" | "past" | "all";

const TABS: { id: Timeframe; label: string }[] = [
    { id: "upcoming", label: "Upcoming" },
    { id: "past", label: "Past" },
    { id: "all", label: "All" },
];

const STATUS_STYLE: Record<MeetingStatus, { label: string; cls: string }> = {
    booked: { label: "Booked", cls: "bg-sky-50 text-sky-700 border-sky-200" },
    rescheduled: { label: "Rescheduled", cls: "bg-amber-50 text-amber-700 border-amber-200" },
    canceled: { label: "Canceled", cls: "bg-slate-100 text-slate-500 border-slate-200" },
    completed: { label: "Completed", cls: "bg-emerald-50 text-emerald-700 border-emerald-200" },
    no_show: { label: "No-show", cls: "bg-red-50 text-red-700 border-red-200" },
};

function formatWhen(iso?: string): { date: string; time: string; rel: string } {
    if (!iso) return { date: "No time set", time: "", rel: "" };
    const d = new Date(iso);
    if (isNaN(d.getTime())) return { date: "No time set", time: "", rel: "" };
    const date = d.toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" });
    const time = d.toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" });
    const diffDays = Math.round((d.getTime() - Date.now()) / 86_400_000);
    let rel = "";
    if (Math.abs(diffDays) <= 14) {
        rel = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" }).format(diffDays, "day");
    }
    return { date, time, rel };
}

// --- Add to calendar (no API: a Google template link + a downloadable .ics) ---

function gcalStamp(iso: string): string {
    return new Date(iso).toISOString().replace(/[-:]/g, "").replace(/\.\d{3}/, "");
}

function endStamp(m: MeetingBooking): string {
    if (m.end_time) return gcalStamp(m.end_time);
    if (m.scheduled_for) return gcalStamp(new Date(new Date(m.scheduled_for).getTime() + 30 * 60_000).toISOString());
    return "";
}

function googleCalURL(m: MeetingBooking): string {
    const params = new URLSearchParams({ action: "TEMPLATE", text: m.event_name || "Meeting" });
    if (m.scheduled_for) params.set("dates", `${gcalStamp(m.scheduled_for)}/${endStamp(m)}`);
    const details = [m.invitee_name && `With ${m.invitee_name}`, m.invitee_email, m.join_url]
        .filter(Boolean)
        .join("\n");
    if (details) params.set("details", details);
    if (m.location) params.set("location", m.location);
    return `https://calendar.google.com/calendar/render?${params.toString()}`;
}

function downloadICS(m: MeetingBooking) {
    const esc = (s: string) => s.replace(/([,;\\])/g, "\\$1").replace(/\n/g, "\\n");
    const lines = [
        "BEGIN:VCALENDAR",
        "VERSION:2.0",
        "PRODID:-//Warmbly//Meetings//EN",
        "BEGIN:VEVENT",
        `UID:${m.id}@warmbly`,
        m.scheduled_for ? `DTSTART:${gcalStamp(m.scheduled_for)}` : "",
        `DTEND:${endStamp(m)}`,
        `SUMMARY:${esc(m.event_name || "Meeting")}`,
        m.location || m.join_url ? `LOCATION:${esc(m.location || m.join_url || "")}` : "",
        m.join_url ? `URL:${m.join_url}` : "",
        "END:VEVENT",
        "END:VCALENDAR",
    ].filter(Boolean);
    const blob = new Blob([lines.join("\r\n")], { type: "text/calendar;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `${(m.event_name || "meeting").replace(/[^\w-]+/g, "_")}.ics`;
    a.click();
    URL.revokeObjectURL(url);
}

export default function MeetingsPage() {
    const [timeframe, setTimeframe] = React.useState<Timeframe>("upcoming");
    const [searchRaw, setSearchRaw] = React.useState("");
    const [search, setSearch] = React.useState("");
    const [creating, setCreating] = React.useState(false);

    React.useEffect(() => {
        const t = setTimeout(() => setSearch(searchRaw.trim()), 250);
        return () => clearTimeout(t);
    }, [searchRaw]);

    const filters: MeetingsSearch = React.useMemo(
        () => ({ timeframe: timeframe === "all" ? "" : timeframe, q: search || undefined }),
        [timeframe, search],
    );

    const { meetings, total, isLoading, fetchNextPage, hasNextPage, isFetchingNextPage } =
        useSearchMeetings({ filters });
    const { data: summary } = useMeetingsSummary();

    const rows = meetings ?? [];

    return (
        <Page>
            <PageTopbar eyebrow="Meetings / Calls" subtitle="Calls you schedule, and calls prospects book with you">
                <TopbarAction href="/app/integrations" variant="ghost" icon={<CableIcon className="w-3.5 h-3.5" />}>
                    Calendars
                </TopbarAction>
                <TopbarAction onClick={() => setCreating(true)} icon={<PlusIcon className="w-3.5 h-3.5" />}>
                    New meeting
                </TopbarAction>
            </PageTopbar>

            <StatStrip cols={4}>
                <Stat label="Upcoming" accent={(summary?.upcoming ?? 0) > 0} value={<AnimatedNumber value={summary?.upcoming ?? 0} />} />
                <Stat label="Today" value={<AnimatedNumber value={summary?.today ?? 0} />} />
                <Stat label="Total booked" value={<AnimatedNumber value={summary?.total ?? 0} />} />
                <Stat label="Canceled" value={<AnimatedNumber value={summary?.canceled ?? 0} />} last />
            </StatStrip>

            <PageBody>
                <SectionBar label="Meetings" count={total ? `${rows.length} of ${total}` : undefined}>
                    <div className="flex items-center gap-0.5 rounded-md border border-slate-200 p-0.5">
                        {TABS.map((tab) => (
                            <button
                                key={tab.id}
                                type="button"
                                onClick={() => setTimeframe(tab.id)}
                                className={cn(
                                    "h-6 px-2.5 rounded text-[11.5px] font-medium transition-colors",
                                    timeframe === tab.id
                                        ? "bg-sky-600 text-white"
                                        : "text-slate-500 hover:text-slate-900 hover:bg-slate-100",
                                )}
                            >
                                {tab.label}
                            </button>
                        ))}
                    </div>
                    <SearchInput
                        value={searchRaw}
                        onChange={(v) => setSearchRaw(v)}
                        placeholder="Search name, email, or event…"
                        className="w-full sm:w-56"
                    />
                </SectionBar>

                {isLoading ? (
                    <div className="px-5 py-16 flex justify-center">
                        <Loader2Icon className="w-5 h-5 text-slate-300 animate-spin" />
                    </div>
                ) : rows.length === 0 ? (
                    <EmptyBlock
                        title={timeframe === "upcoming" ? "No upcoming meetings" : "No meetings yet"}
                        body="Schedule a call with a contact, or connect Calendly / Cal.com so calls prospects book land here automatically."
                        cta={
                            <>
                                <TopbarAction onClick={() => setCreating(true)} icon={<PlusIcon className="w-3.5 h-3.5" />}>
                                    New meeting
                                </TopbarAction>
                                <TopbarAction href="/app/integrations" variant="ghost" icon={<CableIcon className="w-3.5 h-3.5" />}>
                                    Connect a calendar
                                </TopbarAction>
                            </>
                        }
                    />
                ) : (
                    <div>
                        <div className="h-8 px-5 flex items-center gap-3 border-b border-slate-200 bg-slate-50/60 text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                            <span className="w-28 md:w-40 shrink-0">When</span>
                            <span className="flex-1 min-w-0">Contact</span>
                            <span className="hidden md:block flex-1 min-w-0">Meeting</span>
                            <span className="hidden md:block w-24 shrink-0">Source</span>
                            <span className="w-20 md:w-28 shrink-0">Status</span>
                            <span className="w-auto md:w-28 shrink-0 text-right">Actions</span>
                        </div>
                        {rows.map((m) => (
                            <MeetingRow key={m.id} m={m} />
                        ))}
                        {hasNextPage && (
                            <div className="px-5 py-4 flex justify-center">
                                <button
                                    type="button"
                                    onClick={() => fetchNextPage()}
                                    disabled={isFetchingNextPage}
                                    className="h-7 px-3 rounded-md border border-slate-200 text-[12px] text-slate-600 hover:text-slate-900 hover:border-slate-300 inline-flex items-center gap-1.5 disabled:opacity-60"
                                >
                                    {isFetchingNextPage && <Loader2Icon className="w-3.5 h-3.5 animate-spin" />}
                                    Load more
                                </button>
                            </div>
                        )}
                    </div>
                )}
            </PageBody>

            <NewMeetingDialog open={creating} onClose={() => setCreating(false)} />
        </Page>
    );
}

function MeetingRow({ m }: { m: MeetingBooking }) {
    const when = formatWhen(m.scheduled_for);
    const status = STATUS_STYLE[m.status] ?? STATUS_STYLE.booked;
    const contactLabel = m.contact_name || m.invitee_name || m.invitee_email || "Unknown";
    const canceled = m.status === "canceled";
    const isManual = m.source === "manual";
    const confirm = useConfirm();
    const del = useDeleteMeeting();

    const remove = () =>
        confirm.show("Delete this meeting? This only removes it from Warmbly.", async () => {
            await del.mutateAsync(m.id);
            toast.success("Meeting deleted");
        });

    return (
        <div className="group min-h-11 px-5 py-1.5 flex items-center gap-3 border-b border-slate-200/60 hover:bg-slate-50/80 transition-colors">
            <div className="w-28 md:w-40 shrink-0">
                <div className="flex items-center gap-1.5 text-[12.5px] text-slate-800">
                    <CalendarClockIcon className="w-3.5 h-3.5 text-slate-400 shrink-0" />
                    <span className="truncate">{when.date}</span>
                </div>
                <div className="text-[11px] text-slate-400 pl-5 truncate">
                    {when.time}
                    {when.rel ? ` · ${when.rel}` : ""}
                </div>
            </div>

            <div className="flex-1 min-w-0">
                <div className="text-[12.5px] text-slate-900 font-medium truncate">{contactLabel}</div>
                <div className="text-[11px] text-slate-400 truncate">{m.invitee_email}</div>
            </div>

            <div className="hidden md:block flex-1 min-w-0">
                <div className="text-[12.5px] text-slate-700 truncate">{m.event_name || "Meeting"}</div>
                {m.location && <div className="text-[11px] text-slate-400 truncate">{m.location}</div>}
            </div>

            <div className="hidden md:block w-24 shrink-0">
                <span className="text-[11.5px] text-slate-500">
                    {isManual ? "Manual" : PROVIDER_LABELS[m.source as keyof typeof PROVIDER_LABELS] ?? m.source}
                </span>
            </div>

            <div className="w-20 md:w-28 shrink-0">
                <span className={cn("inline-flex items-center h-5 px-2 rounded border text-[10.5px] font-medium", status.cls)}>
                    {status.label}
                </span>
            </div>

            <div className="w-auto md:w-28 shrink-0 flex items-center justify-end gap-1">
                {!canceled && m.scheduled_for && (
                    <PopoverMenu align="end" side="bottom">
                        <PopoverMenuTrigger asChild>
                            <button
                                type="button"
                                title="Add to your calendar"
                                className="h-6 w-6 rounded inline-flex items-center justify-center text-slate-400 hover:text-sky-600 hover:bg-sky-50"
                            >
                                <CalendarPlusIcon className="w-3.5 h-3.5" />
                            </button>
                        </PopoverMenuTrigger>
                        <PopoverMenuContent>
                            <PopoverMenuItem
                                onSelect={() => window.open(googleCalURL(m), "_blank", "noopener,noreferrer")}
                            >
                                Google Calendar
                            </PopoverMenuItem>
                            <PopoverMenuItem onSelect={() => downloadICS(m)}>Download .ics</PopoverMenuItem>
                        </PopoverMenuContent>
                    </PopoverMenu>
                )}
                {!canceled && m.join_url && (
                    <a
                        href={m.join_url}
                        target="_blank"
                        rel="noopener noreferrer"
                        title="Join call"
                        className="h-6 w-6 rounded inline-flex items-center justify-center text-slate-400 hover:text-sky-600 hover:bg-sky-50"
                    >
                        <VideoIcon className="w-3.5 h-3.5" />
                    </a>
                )}
                {!canceled && m.reschedule_url && (
                    <a
                        href={m.reschedule_url}
                        target="_blank"
                        rel="noopener noreferrer"
                        title="Reschedule"
                        className="h-6 w-6 rounded inline-flex items-center justify-center text-slate-400 hover:text-amber-600 hover:bg-amber-50"
                    >
                        <RotateCcwIcon className="w-3.5 h-3.5" />
                    </a>
                )}
                {!canceled && !isManual && m.cancel_url && (
                    <a
                        href={m.cancel_url}
                        target="_blank"
                        rel="noopener noreferrer"
                        title="Cancel"
                        className="h-6 w-6 rounded inline-flex items-center justify-center text-slate-400 hover:text-red-600 hover:bg-red-50"
                    >
                        <XCircleIcon className="w-3.5 h-3.5" />
                    </a>
                )}
                {isManual && (
                    <button
                        type="button"
                        onClick={remove}
                        title="Delete meeting"
                        className="h-6 w-6 rounded inline-flex items-center justify-center text-slate-400 hover:text-red-600 hover:bg-red-50"
                    >
                        <Trash2Icon className="w-3.5 h-3.5" />
                    </button>
                )}
            </div>
        </div>
    );
}
