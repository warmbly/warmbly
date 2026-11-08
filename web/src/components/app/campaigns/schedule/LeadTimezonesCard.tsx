import React from "react";
import { ChevronDownIcon, Clock3Icon, GlobeIcon, MapPinOffIcon } from "lucide-react";
import type { TimezoneMode } from "@/lib/api/models/app/campaigns/Campaign";
import useCampaignLeadTimezones from "@/lib/api/hooks/app/campaigns/useCampaignLeadTimezones";
import type { Interval } from "./WeekScheduleGrid";
import { inWindow, localTime, opensOnTheirClock, windowOnTheirClock, zoneLabel } from "./leadClock";

const regionNames = typeof Intl.DisplayNames === "function" ? new Intl.DisplayNames(["en"], { type: "region" }) : null;
const countryName = (code: string) => (code ? (regionNames?.of(code) ?? code) : "");

const MODES: { value: TimezoneMode; title: string; body: (zone: string) => string }[] = [
    {
        value: "campaign",
        title: "Campaign timezone",
        body: (zone) => `Every lead gets mail inside these windows on ${zone}.`,
    },
    {
        value: "recipient",
        title: "Each lead's local time",
        body: (zone) => `A 9–5 window lands 9–5 in each recipient's own day. Leads with no location are sent on ${zone}.`,
    },
];

const opensLabel = (opens: string | null) => (opens ? `Opens ${opens} their time` : "No sending days");

// How many zones show before "more".
const SHOWN = 6;

export default function LeadTimezonesCard({
    campaignId,
    mode,
    onModeChange,
    windows,
    campaignZone,
    zonePhrase,
}: {
    campaignId: string;
    mode: TimezoneMode;
    onModeChange: (mode: TimezoneMode) => void;
    /** The draft windows, Monday first, so the breakdown follows unsaved edits. */
    windows: Interval[][];
    /** The campaign's effective IANA zone. */
    campaignZone: string;
    zonePhrase: string;
}) {
    const q = useCampaignLeadTimezones(campaignId);
    const [expanded, setExpanded] = React.useState(false);
    const [now, setNow] = React.useState(() => new Date());
    React.useEffect(() => {
        const t = window.setInterval(() => setNow(new Date()), 60_000);
        return () => window.clearInterval(t);
    }, []);

    const data = q.data;
    const zones = data?.zones ?? [];
    const shown = expanded ? zones : zones.slice(0, SHOWN);
    const hidden = zones.length - shown.length;
    const max = Math.max(1, ...zones.map((z) => z.leads));
    // Leads with no location are on the campaign's clock.
    const openNow =
        mode === "recipient"
            ? zones.filter((z) => inWindow(windows, z.timezone, now)).reduce((s, z) => s + z.leads, 0) +
              (inWindow(windows, campaignZone, now) ? (data?.unknown ?? 0) : 0)
            : 0;

    return (
        <section aria-label="Whose time" className="rounded-lg border border-slate-200 bg-white overflow-hidden">
            <div className="px-4 pt-3 pb-3 border-b border-slate-200/70">
                <div className="flex items-center gap-2 mb-2.5">
                    <GlobeIcon className="w-3.5 h-3.5 text-slate-400" />
                    <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Whose time</span>
                </div>
                <div role="radiogroup" aria-label="Whose time the windows are read in" className="grid gap-2 sm:grid-cols-2">
                    {MODES.map((m) => {
                        const active = mode === m.value;
                        return (
                            // A real radio under the card, so arrow keys and the single tab stop are native.
                            <label
                                key={m.value}
                                className={`block cursor-pointer text-left rounded-md border px-3 py-2.5 transition-colors has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-sky-200 ${
                                    active ? "border-sky-400 bg-sky-50/60 ring-2 ring-sky-100" : "border-slate-200 hover:border-slate-300"
                                }`}
                            >
                                <input
                                    type="radio"
                                    name={`timezone-mode-${campaignId}`}
                                    value={m.value}
                                    checked={active}
                                    onChange={() => onModeChange(m.value)}
                                    className="sr-only"
                                />
                                <span className="flex items-center gap-2">
                                    <span
                                        className={`size-3.5 rounded-full border inline-flex items-center justify-center shrink-0 ${
                                            active ? "border-sky-600" : "border-slate-300"
                                        }`}
                                    >
                                        {active && <span className="size-1.5 rounded-full bg-sky-600" />}
                                    </span>
                                    <span className={`text-[12.5px] ${active ? "text-slate-900 font-medium" : "text-slate-700"}`}>
                                        {m.title}
                                    </span>
                                </span>
                                <span className="block text-[11.5px] leading-snug text-slate-500 mt-1 pl-5.5">{m.body(zonePhrase)}</span>
                            </label>
                        );
                    })}
                </div>
                {mode === "recipient" && (
                    <p className="mt-2 text-[11px] leading-snug text-slate-400">
                        Each mailbox still sends only inside its own hours (its working hours, or 8am to 8pm in its timezone), so a lead
                        whose day never meets them waits for a mailbox that can reach them. For leads far from your mailboxes, add mailboxes set to their timezone, or run
                        a campaign per region.
                    </p>
                )}
            </div>

            <div className="px-4 py-3">
                <div className="flex flex-wrap items-center gap-x-2 gap-y-1 mb-2">
                    <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Where your leads are</span>
                    {data && data.total > 0 && mode === "recipient" && (
                        <span className="ml-auto inline-flex items-center gap-1 text-[11px] text-slate-500">
                            <Clock3Icon className="w-3 h-3" />
                            <span className="tabular-nums font-medium text-slate-700">{openNow.toLocaleString()}</span> inside their
                            window right now
                        </span>
                    )}
                </div>

                {q.isPending ? (
                    <div className="space-y-1.5">
                        {[0, 1, 2].map((i) => (
                            <div key={i} className="h-6 rounded bg-slate-100 animate-pulse" />
                        ))}
                    </div>
                ) : q.isError && !data ? (
                    <p className="text-[12px] text-slate-500">
                        Could not load where this campaign's leads are.{" "}
                        <button type="button" onClick={() => q.refetch()} className="text-sky-700 hover:text-sky-800 font-medium">
                            Try again
                        </button>
                    </p>
                ) : !data || data.total === 0 ? (
                    <p className="text-[12px] text-slate-500">
                        Add leads to see their timezones. Warmbly reads a timezone, country, state, city or location column from your import,
                        then the phone number and the email domain.
                    </p>
                ) : (
                    <>
                        <ul className="space-y-1">
                            {shown.map((z) => {
                                const open = inWindow(windows, z.timezone, now);
                                const theirs = mode === "campaign" ? windowOnTheirClock(windows, campaignZone, z.timezone, now) : null;
                                return (
                                    <li
                                        key={z.timezone}
                                        className="grid grid-cols-[minmax(0,1fr)_auto] sm:grid-cols-[minmax(0,1fr)_88px_250px] items-center gap-x-3 h-7 text-[12px]"
                                    >
                                        <span className="relative min-w-0 h-6 flex items-center px-2 rounded">
                                            <span className="absolute inset-y-0 left-0 rounded bg-sky-50" style={{ width: `${(z.leads / max) * 100}%` }} />
                                            <span className="relative truncate text-slate-800">
                                                {zoneLabel(z.timezone)}
                                                {z.country && <span className="text-slate-400"> · {countryName(z.country)}</span>}
                                            </span>
                                            <span className="relative ml-auto pl-2 tabular-nums text-slate-600">{z.leads.toLocaleString()}</span>
                                        </span>
                                        <span className="hidden sm:inline tabular-nums text-slate-500">{localTime(z.timezone, now)} there</span>
                                        <span className="justify-self-end text-[11px]">
                                            {mode === "recipient" ? (
                                                <span className={open ? "text-emerald-600" : "text-slate-500 tabular-nums"}>
                                                    {open ? "In their window" : opensLabel(opensOnTheirClock(windows, z.timezone, now))}
                                                </span>
                                            ) : theirs ? (
                                                <span className="text-slate-500 tabular-nums" title="The campaign's next window, on their clock">
                                                    Lands {theirs} their time
                                                </span>
                                            ) : (
                                                <span className="text-slate-400">No sending days</span>
                                            )}
                                        </span>
                                    </li>
                                );
                            })}
                        </ul>
                        {hidden > 0 && (
                            <button
                                type="button"
                                onClick={() => setExpanded(true)}
                                className="mt-1 inline-flex items-center gap-1 text-[11.5px] text-sky-700 hover:text-sky-800"
                            >
                                <ChevronDownIcon className="w-3 h-3" />
                                {hidden} more timezone{hidden === 1 ? "" : "s"}
                            </button>
                        )}
                        {data.unknown > 0 && (
                            <div className="mt-2.5 flex items-start gap-2 rounded-md border border-amber-200 bg-amber-50/60 px-2.5 py-2 text-[11.5px] leading-snug text-amber-800">
                                <MapPinOffIcon className="w-3.5 h-3.5 mt-px shrink-0" />
                                <span>
                                    <span className="font-medium tabular-nums">{data.unknown.toLocaleString()}</span> of{" "}
                                    {data.total.toLocaleString()} leads have no location, so they are sent on {zonePhrase}. Import a country,
                                    state, city or timezone column, or add one to the contact, to place them.
                                </span>
                            </div>
                        )}
                        {data.resolving > 0 && (
                            <p className="mt-2 text-[11px] text-slate-400">
                                Reading the location of {data.resolving.toLocaleString()} recently changed lead
                                {data.resolving === 1 ? "" : "s"}…
                            </p>
                        )}
                    </>
                )}
            </div>
        </section>
    );
}
