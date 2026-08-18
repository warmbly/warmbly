import { motion } from "framer-motion";
import { CheckCircle2Icon, DownloadIcon, HourglassIcon, RefreshCwIcon } from "lucide-react";
import useSync from "@/lib/api/hooks/app/emails/useSync";
import type { SyncThrottleReason } from "@/lib/api/models/app/emails/SyncState";
import { cn } from "@/lib/utils";

// Sync card in the mailbox drawer: what the initial import has done, whether
// fair use is holding new mail, and when the last pass ran. Copy stays
// concrete (numbers, times) because "syncing..." with no progress is what
// makes a fresh mailbox feel broken.

const REASON_COPY: Record<SyncThrottleReason, string> = {
    burst: "a lot of mail arrived at once",
    hourly: "the hourly limit for this mailbox was reached",
    daily: "the daily limit for this mailbox was reached",
    org_daily: "the workspace's daily limit was reached",
    priority_daily: "the daily limit for replies was reached",
};

function relative(iso: string): string {
    const diff = Date.now() - new Date(iso).getTime();
    const m = Math.round(diff / 60_000);
    if (m < 1) return "just now";
    if (m < 60) return `${m} min ago`;
    const h = Math.round(m / 60);
    if (h < 24) return `${h} h ago`;
    return new Date(iso).toLocaleDateString();
}

function until(iso: string): string {
    const d = new Date(iso);
    const sameDay = d.toDateString() === new Date().toDateString();
    return sameDay
        ? d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
        : d.toLocaleString([], { weekday: "short", hour: "2-digit", minute: "2-digit" });
}

export default function SyncStatusCard({ mailboxId }: { mailboxId: string }) {
    const sync = useSync(mailboxId);
    const state = sync.data?.state ?? null;
    const policy = sync.data?.policy;

    if (sync.isPending) {
        return (
            <div className="px-5 py-4">
                <div className="h-3 w-24 rounded bg-slate-100 animate-pulse" />
                <div className="mt-3 h-3 w-48 rounded bg-slate-100 animate-pulse" />
            </div>
        );
    }
    if (!sync.data) return null;

    const throttled = !!state?.throttled_until && new Date(state.throttled_until).getTime() > Date.now();
    const status = state?.backfill_status ?? "pending";
    const cap = policy?.backfill_messages ?? 0;
    const synced = state?.backfill_synced ?? 0;
    const pct = cap > 0 ? Math.min(100, Math.round((synced / cap) * 100)) : 0;

    let headline: React.ReactNode;
    let Icon = CheckCircle2Icon;
    let tone = "text-emerald-600";
    if (throttled && state?.throttled_until) {
        Icon = HourglassIcon;
        tone = "text-amber-600";
        const reason = state.throttle_reason ? REASON_COPY[state.throttle_reason as SyncThrottleReason] : undefined;
        headline = (
            <>
                Waiting on the sync budget until {until(state.throttled_until)}
                {reason ? <span className="text-slate-500"> ({reason})</span> : null}
            </>
        );
    } else if (status === "complete") {
        headline = state?.last_synced_at ? `Up to date, last checked ${relative(state.last_synced_at)}` : "Up to date";
    } else if (status === "running") {
        Icon = DownloadIcon;
        tone = "text-sky-600";
        headline = `Importing recent mail: ${synced.toLocaleString()} message${synced === 1 ? "" : "s"} so far`;
    } else {
        Icon = RefreshCwIcon;
        tone = "text-sky-600";
        headline = "Import starts on the next pass";
    }

    return (
        <div className="px-5 py-4">
            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Sync</div>
            <div className={cn("mt-2 inline-flex items-start gap-1.5 text-[12.5px] font-medium", tone)}>
                <Icon className={cn("w-3.5 h-3.5 mt-0.5 shrink-0", status === "running" && !throttled && "animate-pulse")} />
                <span className="text-slate-900">{headline}</span>
            </div>

            {status === "running" && !throttled && (
                <div className="mt-2.5 h-1.5 w-full rounded-full bg-slate-100 overflow-hidden">
                    <motion.div
                        className="h-full rounded-full bg-sky-500"
                        initial={false}
                        animate={{ width: `${Math.max(pct, 3)}%` }}
                        transition={{ type: "spring", stiffness: 120, damping: 20 }}
                    />
                </div>
            )}

            <p className="mt-2 text-[11.5px] leading-relaxed text-slate-500">
                {status === "complete" && policy
                    ? `Imported ${synced.toLocaleString()} message${synced === 1 ? "" : "s"} from the last ${policy.backfill_days} days. New mail syncs as it arrives.`
                    : policy
                        ? `The last ${policy.backfill_days} days come in newest first, up to ${cap.toLocaleString()} messages. New mail syncs alongside.`
                        : null}
                {throttled ? " Replies to your outreach keep syncing; the rest resumes automatically." : null}
            </p>

            {(state?.deferred ?? 0) > 0 && (
                <p className="mt-1 text-[11.5px] text-amber-700">
                    {state!.deferred.toLocaleString()} message{state!.deferred === 1 ? "" : "s"} waiting on the server.
                </p>
            )}
        </div>
    );
}
