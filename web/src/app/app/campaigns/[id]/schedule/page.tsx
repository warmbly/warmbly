import React from "react";
import { ArrowRightIcon, CalendarClockIcon, CalendarRangeIcon, GlobeIcon } from "lucide-react";
import { differenceInCalendarDays, format } from "date-fns";
import DateSelect from "@/components/app/campaigns/schedule/ScheduleDateSelect";
import WeekScheduleGrid, { type Interval } from "@/components/app/campaigns/schedule/WeekScheduleGrid";
import { Loading } from "@/components/loader";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuTrigger,
    SelectButton,
} from "@/components/ui/popover-menu";
import { useCampaign } from "@/hooks/context/campaign";
import type Campaign from "@/lib/api/models/app/campaigns/Campaign";
import type { ScheduleInterval } from "@/lib/api/models/app/campaigns/Campaign";
import useUpdateCampaign from "@/lib/api/hooks/app/campaigns/useUpdateCampaign";
import toast from "react-hot-toast";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { useUserProfile } from "@/hooks/context/user";

// ── Wire ↔ display conversion ────────────────────────────────────────────
// Wire (schedule_windows) is a 7-array indexed by weekday 0=Sun..6=Sat (the
// backend's time.Weekday). The grid is Monday-first (column 0 = Mon), so
// display index i maps to wire weekday (i+1)%7.
const parseHHMM = (t: string): number => {
    const [h, m] = (t || "0:0").split(":").map(Number);
    return (h || 0) * 60 + (m || 0);
};

function wireToDisplay(wire: ScheduleInterval[][] | null | undefined): Interval[][] {
    return Array.from({ length: 7 }, (_, i) => {
        const day = wire?.[(i + 1) % 7] ?? [];
        return day.map((iv) => ({ start: iv.start, end: iv.end }));
    });
}

function displayToWire(display: Interval[][]): ScheduleInterval[][] {
    const wire: ScheduleInterval[][] = Array.from({ length: 7 }, () => []);
    for (let i = 0; i < 7; i++) wire[(i + 1) % 7] = display[i].map((iv) => ({ start: iv.start, end: iv.end }));
    return wire;
}

const hasAnyWindow = (w: ScheduleInterval[][] | null | undefined): boolean =>
    !!w && w.some((d) => (d?.length ?? 0) > 0);

// Seed the editor: prefer schedule_windows; otherwise derive one window per
// active legacy day (days bitmask is Monday-indexed, matching the grid).
function seedWindows(c: Campaign): Interval[][] {
    if (hasAnyWindow(c.schedule_windows)) return wireToDisplay(c.schedule_windows);
    const start = parseHHMM(c.start_time);
    const end = parseHHMM(c.end_time);
    return Array.from({ length: 7 }, (_, i) => {
        const active = (c.days & (1 << i)) !== 0;
        return active && end > start ? [{ start, end }] : [];
    });
}

const PRESETS: { label: string; build: () => Interval[][] }[] = [
    {
        label: "Mon–Fri 9–5",
        build: () => Array.from({ length: 7 }, (_, i) => (i < 5 ? [{ start: 540, end: 1020 }] : [])),
    },
    {
        label: "Every day 9–5",
        build: () => Array.from({ length: 7 }, () => [{ start: 540, end: 1020 }]),
    },
    { label: "Clear", build: () => Array.from({ length: 7 }, () => []) },
];

export default function CampaignSchedule() {
    const campaign = useCampaign();
    if (!campaign) {
        throw new Error("CampaignSchedule cannot be rendered without a campaign");
    }

    const u = useUserProfile();
    const updateCampaign = useUpdateCampaign(campaign.id);

    const [loading, setLoading] = React.useState(false);
    const [newData, setNewData] = React.useState(campaign); // timezone + dates draft
    const [windows, setWindows] = React.useState<Interval[][]>(() => seedWindows(campaign));

    const baseline = React.useMemo(() => seedWindows(campaign), [campaign]);

    React.useEffect(() => {
        setNewData(campaign);
        setWindows(seedWindows(campaign));
    }, [campaign]);

    const windowsChanged = React.useMemo(
        () => JSON.stringify(windows) !== JSON.stringify(baseline),
        [windows, baseline],
    );

    const fieldChanges = (): Partial<Campaign> => ({
        ...(newData.start_date !== campaign.start_date && { start_date: newData.start_date }),
        ...(newData.end_date !== campaign.end_date && { end_date: newData.end_date }),
        ...(newData.timezone !== campaign.timezone && { timezone: newData.timezone }),
    });

    const hasChanges = windowsChanged || Object.keys(fieldChanges()).length > 0;

    const totalWindows = windows.reduce((s, d) => s + d.length, 0);
    const activeDays = windows.filter((d) => d.length > 0).length;

    async function submit() {
        if (loading) return;
        if (totalWindows === 0) {
            toast.error("Add at least one sending window.");
            return;
        }
        const patch: Partial<Campaign> = {
            ...fieldChanges(),
            ...(windowsChanged && { schedule_windows: displayToWire(windows) }),
        };
        try {
            setLoading(true);
            await toast.promise(updateCampaign.mutateAsync(patch), {
                loading: "Saving…",
                success: "Schedule updated.",
                error: (err: AppError) => buildError(err),
            });
        } finally {
            setLoading(false);
        }
    }

    const tzLabel = u.timezones.find((tz) => tz.name === newData.timezone)?.display_name ?? newData.timezone;

    const startDate = newData.start_date instanceof Date ? newData.start_date : null;
    const endDate = newData.end_date instanceof Date ? newData.end_date : null;
    const durationLabel =
        startDate && endDate ? `${differenceInCalendarDays(endDate, startDate)} days` : "Open-ended";
    const datesHint = startDate
        ? endDate
            ? `${format(startDate, "MMM d")} – ${format(endDate, "MMM d, yyyy")}`
            : `from ${format(startDate, "MMM d, yyyy")} onward`
        : endDate
            ? `until ${format(endDate, "MMM d, yyyy")}`
            : "Runs continuously while active";

    return (
        <div className="space-y-4">
            {/* Hero scheduler card */}
            <section className="rounded-lg border border-slate-200 bg-white overflow-hidden">
                <div className="px-4 py-3 sm:py-0 sm:h-14 flex flex-col gap-2 sm:flex-row sm:items-center sm:gap-3 border-b border-slate-200/70">
                    <div className="flex items-center gap-3 min-w-0">
                        <span className="size-7 rounded-md bg-sky-50 text-sky-600 inline-flex items-center justify-center shrink-0">
                            <CalendarClockIcon className="w-4 h-4" />
                        </span>
                        <div className="min-w-0">
                        <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                            Weekly sending windows
                        </div>
                        {totalWindows === 0 ? (
                            <p className="text-[12.5px] text-rose-500 leading-tight mt-0.5">
                                No windows yet — drag on a day to add one.
                            </p>
                        ) : (
                            <p className="text-[12.5px] text-slate-700 leading-tight mt-0.5">
                                <span className="font-medium text-slate-900">{totalWindows}</span> window
                                {totalWindows === 1 ? "" : "s"} across{" "}
                                <span className="font-medium text-slate-900">{activeDays}</span> day
                                {activeDays === 1 ? "" : "s"}
                            </p>
                        )}
                        </div>
                    </div>
                    <div className="w-full sm:w-auto sm:ml-auto shrink-0">
                        <PopoverMenu>
                            <PopoverMenuTrigger asChild>
                                <SelectButton
                                    icon={<GlobeIcon className="w-3.5 h-3.5" />}
                                    label={tzLabel}
                                    className="w-full justify-between sm:w-auto sm:max-w-[230px]"
                                />
                            </PopoverMenuTrigger>
                            <PopoverMenuContent minWidth={320} className="max-h-72 overflow-y-auto">
                                {u.timezones.map((tz) => (
                                    <PopoverMenuItem
                                        key={tz.name}
                                        selected={tz.name === newData.timezone}
                                        onSelect={() => setNewData((bef) => ({ ...bef, timezone: tz.name }))}
                                    >
                                        {tz.display_name}
                                    </PopoverMenuItem>
                                ))}
                            </PopoverMenuContent>
                        </PopoverMenu>
                    </div>
                </div>

                {/* presets */}
                <div className="px-4 py-2.5 flex flex-wrap items-center gap-x-3 gap-y-2 border-b border-slate-200/70 bg-slate-50/30">
                    <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium mr-0.5">
                        Presets
                    </span>
                    {PRESETS.map((p) => (
                        <button
                            key={p.label}
                            type="button"
                            onClick={() => setWindows(p.build())}
                            className="h-7 px-2.5 rounded-md border border-slate-200 text-[11.5px] text-slate-600 hover:border-slate-300 hover:text-slate-900 transition-colors"
                        >
                            {p.label}
                        </button>
                    ))}
                    <span className="ml-auto text-[11px] text-slate-400 hidden sm:block">
                        Drag to add · drag a block to move · edges to resize · ⧉ copies a day to all
                    </span>
                </div>

                {/* the visual week grid (scrolls horizontally on narrow screens
                    so the 7 day columns stay tappable) */}
                <div className="px-4 py-4">
                    <div className="overflow-x-auto -mx-1 px-1">
                        <WeekScheduleGrid windows={windows} onChange={setWindows} />
                    </div>
                    <p className="text-[11px] text-slate-400 mt-3">
                        Each day is independent — set different windows per day, or several windows in one day. Sends
                        are scheduled in {tzLabel}; worker IPs spread distribution naturally.
                    </p>
                </div>
            </section>

            {/* Run dates */}
            <section className="rounded-lg border border-slate-200 bg-white px-4 pt-3 pb-4">
                <div className="flex flex-wrap items-center gap-x-2 gap-y-1 mb-2.5">
                    <CalendarRangeIcon className="w-3.5 h-3.5 text-slate-400" />
                    <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                        Campaign dates
                    </span>
                    <span className="ml-auto inline-flex items-center gap-2 text-[11px] text-slate-400">
                        <span>{datesHint}</span>
                        <span className="inline-flex items-center h-5 px-1.5 rounded border border-slate-200 bg-slate-50 text-slate-600 tabular-nums">
                            {durationLabel}
                        </span>
                    </span>
                </div>
                <div className="flex items-end gap-3 flex-wrap">
                    <div className="w-full sm:w-[170px]">
                        <DateSelect
                            title="Start date"
                            value={newData.start_date ?? null}
                            onChange={(v) => setNewData((b) => ({ ...b, start_date: v }))}
                        />
                    </div>
                    <ArrowRightIcon className="w-4 h-4 text-slate-300 shrink-0 self-end mb-1.5 hidden sm:block" />
                    <div className="w-full sm:w-[170px]">
                        <DateSelect
                            title="End date"
                            value={newData.end_date ?? null}
                            onChange={(v) => setNewData((b) => ({ ...b, end_date: v }))}
                        />
                    </div>
                    <p className="text-[11px] text-slate-400 self-end mb-1 flex-1 sm:min-w-[180px]">
                        Optional bounds for when the campaign may send. Leave both blank to run open-ended.
                    </p>
                </div>
            </section>

            {/* Save / Reset */}
            <div
                className={`flex justify-end gap-2 pt-1 transition-opacity duration-100 ${
                    hasChanges ? "opacity-100" : "opacity-40 pointer-events-none"
                }`}
            >
                <button
                    className="h-7 px-3 text-[12px] font-medium text-slate-600 hover:text-slate-900 border border-slate-200 hover:border-slate-300 rounded-md transition-colors"
                    onClick={() => {
                        setNewData(campaign);
                        setWindows(seedWindows(campaign));
                    }}
                >
                    Reset
                </button>
                <button
                    className="h-7 px-3 bg-sky-600 hover:bg-sky-700 text-white rounded-md text-[12px] font-medium transition-colors min-w-[110px] inline-flex items-center justify-center"
                    onClick={submit}
                >
                    {loading ? <Loading className="h-4" /> : "Save changes"}
                </button>
            </div>
        </div>
    );
}
