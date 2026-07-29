// /events — the live platform event feed. Every realtime event pushed to
// admin:platform lands here as it happens (fed by RealtimeManager through
// the events ring buffer). Purely event-driven: no polling, no refetch.

import { useMemo, useState } from "react";
import { Pause, Play, Search, Trash2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { PageHeader } from "@/components/layout/PageHeader";
import { cn } from "@/lib/utils";
import {
    MAX_EVENTS,
    clearEvents,
    setPaused,
    useLiveEvents,
    useRealtimeStatus,
    type LiveEvent,
} from "@/lib/realtime/eventsStore";
import type { RealtimeStatus } from "@/lib/realtime/socket";

// ---------------------------------------------------------------- families

type FamilyId = "all" | "emails" | "campaigns" | "accounts" | "audit" | "workers" | "other";

const FAMILY_MATCHERS: { id: Exclude<FamilyId, "all" | "other">; match: (n: string) => boolean }[] = [
    { id: "emails", match: (n) => n.includes("EMAIL") },
    { id: "campaigns", match: (n) => n.includes("CAMPAIGN") },
    { id: "accounts", match: (n) => n.includes("ACCOUNT") || n.includes("WARMUP") },
    { id: "audit", match: (n) => n.includes("AUDIT") },
    { id: "workers", match: (n) => n.includes("WORKER") },
];

const FAMILY_CHIPS: { id: FamilyId; label: string }[] = [
    { id: "all", label: "All" },
    { id: "emails", label: "Emails" },
    { id: "campaigns", label: "Campaigns" },
    { id: "accounts", label: "Accounts & Warmup" },
    { id: "audit", label: "Audit" },
    { id: "workers", label: "Workers" },
    { id: "other", label: "Other" },
];

function inFamily(name: string, family: FamilyId): boolean {
    if (family === "all") return true;
    const upper = name.toUpperCase();
    if (family === "other") return FAMILY_MATCHERS.every((f) => !f.match(upper));
    return FAMILY_MATCHERS.find((f) => f.id === family)!.match(upper);
}

// Badge tone by family; errors win over everything.
function badgeTone(name: string): string {
    const n = name.toUpperCase();
    if (n.includes("ERROR") || n.includes("FAILED")) return "bg-red-100 text-red-700";
    if (n.includes("ACCOUNT") || n.includes("WARMUP")) return "bg-amber-100 text-amber-700";
    if (n.includes("CAMPAIGN")) return "bg-purple-100 text-purple-700";
    if (n.includes("EMAIL")) return "bg-sky-100 text-sky-700";
    if (n.includes("AUDIT")) return "bg-zinc-100 text-zinc-700";
    return "bg-zinc-100 text-zinc-700";
}

// ------------------------------------------------------------------ pieces

const STATUS_META: Record<RealtimeStatus, { label: string; dot: string; text: string }> = {
    connected: { label: "Connected", dot: "bg-emerald-500", text: "text-emerald-700" },
    connecting: { label: "Connecting", dot: "bg-amber-500 animate-pulse", text: "text-amber-700" },
    disconnected: { label: "Disconnected", dot: "bg-red-500", text: "text-red-700" },
};

function StatusPill({ status }: { status: RealtimeStatus }) {
    const meta = STATUS_META[status];
    return (
        <span
            className={cn(
                "inline-flex items-center gap-1.5 rounded-full border border-border bg-card px-2.5 py-1 text-xs font-medium",
                meta.text,
            )}
        >
            <span className={cn("size-2 rounded-full", meta.dot)} />
            {meta.label}
        </span>
    );
}

const ID_FIELDS = ["org_id", "user_id", "campaign_id", "email_account_id"] as const;

function idSummary(payload: Record<string, unknown>): { key: string; value: string }[] {
    const out: { key: string; value: string }[] = [];
    for (const key of ID_FIELDS) {
        const v = payload[key];
        if (typeof v === "string" && v.length > 0) {
            out.push({ key: key.replace(/_id$/, ""), value: v.slice(0, 8) });
        }
    }
    return out;
}

function matchesQuery(ev: LiveEvent, q: string): boolean {
    if (ev.name.toLowerCase().includes(q)) return true;
    for (const value of Object.values(ev.payload)) {
        if (typeof value === "string" && value.toLowerCase().includes(q)) return true;
    }
    return false;
}

function fmtTime(ts: number): string {
    return new Date(ts).toLocaleTimeString(undefined, {
        hour12: false,
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
    });
}

function EventRow({ event }: { event: LiveEvent }) {
    const [expanded, setExpanded] = useState(false);
    const ids = idSummary(event.payload);
    return (
        <li className="border-b border-border/60 last:border-0">
            <button
                type="button"
                onClick={() => setExpanded((v) => !v)}
                className="flex w-full flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2 text-left transition-colors hover:bg-muted/50"
            >
                <span className="w-16 shrink-0 font-mono text-[11px] tabular-nums text-muted-foreground">
                    {fmtTime(event.receivedAt)}
                </span>
                <Badge className={cn("font-mono text-[11px] font-medium", badgeTone(event.name))}>
                    {event.name}
                </Badge>
                <span className="flex min-w-0 flex-wrap items-center gap-x-2.5 font-mono text-[11px] text-muted-foreground">
                    {ids.map((id) => (
                        <span key={id.key}>
                            <span className="opacity-60">{id.key}</span> {id.value}
                        </span>
                    ))}
                </span>
            </button>
            {expanded && (
                <pre className="mx-3 mb-2 max-h-64 overflow-auto rounded-md border border-border bg-muted/40 p-3 font-mono text-[11px] leading-relaxed text-foreground">
                    {JSON.stringify(event.payload, null, 2)}
                </pre>
            )}
        </li>
    );
}

// -------------------------------------------------------------------- page

export default function EventsPage() {
    const { events, paused, missedCount } = useLiveEvents();
    const status = useRealtimeStatus();
    const [query, setQuery] = useState("");
    const [family, setFamily] = useState<FamilyId>("all");

    const filtered = useMemo(() => {
        const q = query.trim().toLowerCase();
        return events.filter(
            (ev) => inFamily(ev.name, family) && (q === "" || matchesQuery(ev, q)),
        );
    }, [events, family, query]);

    return (
        <div>
            <PageHeader
                title="Live Events"
                description="Every realtime event on the platform as it happens, mirrored from the org, user, and entity channels."
            >
                <StatusPill status={status} />
            </PageHeader>

            <div className="mb-3 flex flex-wrap items-center gap-2">
                <div className="relative">
                    <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
                    <Input
                        value={query}
                        onChange={(e) => setQuery(e.target.value)}
                        placeholder="Filter by event name or id"
                        className="h-8 w-64 pl-8 text-[12.5px]"
                    />
                </div>
                <Button
                    variant="outline"
                    size="sm"
                    className="h-8 gap-1.5"
                    onClick={() => setPaused(!paused)}
                >
                    {paused ? <Play className="size-3.5" /> : <Pause className="size-3.5" />}
                    {paused
                        ? `Resume${missedCount > 0 ? ` (${missedCount.toLocaleString()} missed)` : ""}`
                        : "Pause"}
                </Button>
                <Button
                    variant="outline"
                    size="sm"
                    className="h-8 gap-1.5"
                    onClick={clearEvents}
                    disabled={events.length === 0}
                >
                    <Trash2 className="size-3.5" />
                    Clear
                </Button>
            </div>

            <div className="mb-3 flex flex-wrap items-center gap-1.5">
                {FAMILY_CHIPS.map((chip) => (
                    <button
                        key={chip.id}
                        type="button"
                        onClick={() => setFamily(chip.id)}
                        className={cn(
                            "rounded-full border px-2.5 py-1 text-xs transition-colors",
                            family === chip.id
                                ? "border-foreground/20 bg-foreground text-background"
                                : "border-border bg-card text-muted-foreground hover:text-foreground",
                        )}
                    >
                        {chip.label}
                    </button>
                ))}
            </div>

            <div className="overflow-hidden rounded-lg border border-border bg-card">
                {filtered.length === 0 ? (
                    <div className="px-3 py-14 text-center">
                        <div className="text-sm font-medium text-foreground">
                            {events.length === 0 ? "No events yet" : "No matching events"}
                        </div>
                        <div className="mt-0.5 text-xs text-muted-foreground">
                            {events.length === 0
                                ? "Events appear here as platform activity happens. The connection status is shown above."
                                : "Try a different filter or family chip."}
                        </div>
                    </div>
                ) : (
                    <ul>
                        {filtered.map((ev) => (
                            <EventRow key={ev.id} event={ev} />
                        ))}
                    </ul>
                )}
            </div>

            <p className="mt-2 text-[11px] text-muted-foreground">
                Showing the last {Math.min(events.length, MAX_EVENTS).toLocaleString()} events
                (buffer capped at {MAX_EVENTS}). Older events are dropped.
            </p>
        </div>
    );
}
