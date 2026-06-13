import { useMemo } from "react";
import {
    MailOpenIcon,
    MousePointerClickIcon,
    ReplyIcon,
    SendIcon,
    TriangleAlertIcon,
    XCircleIcon,
    type LucideIcon,
} from "lucide-react";
import { useCampaignChannel, type ActivityItem } from "@/hooks/useCampaignChannel";
import useCampaignLogs from "@/lib/api/hooks/app/campaigns/useCampaignLogs";

interface TaskPreviewProps {
    campaignId: string;
    campaignStatus?: string;
}

// ── Status pill tones ───────────────────────────────────────────────
const STATUS_TONE: Record<string, string> = {
    active: "bg-emerald-50 text-emerald-700 ring-1 ring-emerald-200",
    paused: "bg-amber-50 text-amber-700 ring-1 ring-amber-200",
    completed: "bg-sky-50 text-sky-700 ring-1 ring-sky-200",
    draft: "bg-slate-100 text-slate-600 ring-1 ring-slate-200",
};

// ── Activity-type icon + tone ───────────────────────────────────────
const ACTIVITY_META: Record<ActivityItem["type"], { icon: LucideIcon; tone: string }> = {
    sent: { icon: SendIcon, tone: "text-slate-500" },
    opened: { icon: MailOpenIcon, tone: "text-emerald-600" },
    clicked: { icon: MousePointerClickIcon, tone: "text-violet-600" },
    replied: { icon: ReplyIcon, tone: "text-amber-600" },
    bounced: { icon: TriangleAlertIcon, tone: "text-rose-600" },
    failed: { icon: XCircleIcon, tone: "text-rose-600" },
};

function statusLabel(s: string): string {
    return s.charAt(0).toUpperCase() + s.slice(1);
}

function initials(name?: string, email?: string): string {
    const source = (name || email || "").trim();
    if (!source) return "?";
    const parts = source.split(/\s+/).filter(Boolean);
    if (parts.length >= 2) return (parts[0][0] + parts[1][0]).toUpperCase();
    return source[0].toUpperCase();
}

function relativeTime(date: Date): string {
    const diff = Date.now() - date.getTime();
    const sec = Math.round(diff / 1000);
    if (sec < 5) return "just now";
    if (sec < 60) return `${sec}s ago`;
    const min = Math.floor(sec / 60);
    if (min < 60) return `${min}m ago`;
    return date.toLocaleTimeString("en-US", { hour: "2-digit", minute: "2-digit" });
}

function ActivityRow({ activity }: { activity: ActivityItem }) {
    const meta = ACTIVITY_META[activity.type] ?? ACTIVITY_META.sent;
    const Icon = meta.icon;
    return (
        <div className="flex items-start gap-2.5 px-4 py-2 hover:bg-slate-50/80 transition-colors">
            <Icon className={`w-3.5 h-3.5 mt-0.5 shrink-0 ${meta.tone}`} />
            <p className="flex-1 min-w-0 text-[12.5px] text-slate-800 leading-snug break-words">
                {activity.message}
            </p>
            <span className="shrink-0 font-mono text-[10.5px] text-slate-400 tabular-nums mt-0.5">
                {relativeTime(activity.timestamp)}
            </span>
        </div>
    );
}

export default function TaskPreview({ campaignId, campaignStatus: initialStatus }: TaskPreviewProps) {
    const { isConnected, channelState, campaignStatus: realtimeStatus, taskProgress, activities } =
        useCampaignChannel(campaignId);

    // Durable log tail — best effort; never breaks the live panel if it errors.
    const logs = useCampaignLogs(campaignId);

    const currentStatus = realtimeStatus?.status || initialStatus || "draft";
    const isActive = currentStatus === "active";

    const connectionLabel = isConnected
        ? "Connected"
        : channelState === "joining"
          ? "Connecting…"
          : "Disconnected";

    const showNowSending = !!taskProgress && isActive && taskProgress.status === "active";

    const progress = Math.min(100, Math.max(0, taskProgress?.progress ?? 0));
    const processed = taskProgress?.processed_count ?? 0;
    const total = taskProgress?.total_contacts ?? 0;

    // Conservative estimate: remaining contacts paced roughly one per minute.
    const remainingHint = useMemo(() => {
        if (!taskProgress || total <= 0) return null;
        const remaining = total - processed;
        if (remaining <= 0) return null;
        if (remaining < 60) return `~${remaining} min left`;
        const hours = Math.floor(remaining / 60);
        const mins = remaining % 60;
        return mins > 0 ? `~${hours}h ${mins}m left` : `~${hours}h left`;
    }, [taskProgress, total, processed]);

    const recentLogs = useMemo(() => {
        // The client returns logs newest-first, so the first 6 are the latest.
        const all = logs.data?.logs ?? [];
        return all.slice(0, 6);
    }, [logs.data]);

    // Scheduler / send failures surfaced for review, always visible (not hidden
    // behind the live-activity feed) so a stalled or retrying step is never silent.
    const issueLogs = useMemo(() => {
        const all = logs.data?.logs ?? [];
        return all.filter((l) => l.level === "error").slice(0, 3);
    }, [logs.data]);

    const hasActivity = activities.length > 0;

    return (
        <div className="rounded-md border border-slate-200 bg-white overflow-hidden flex flex-col">
            {/* ── Header ─────────────────────────────────────────────── */}
            <div className="shrink-0 px-4 h-11 border-b border-slate-200 flex items-center gap-3">
                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                    Live activity
                </span>
                <span
                    className={`inline-flex items-center px-1.5 h-5 rounded-md text-[10.5px] font-medium ${
                        STATUS_TONE[currentStatus] ?? STATUS_TONE.draft
                    }`}
                >
                    {statusLabel(currentStatus)}
                </span>
                <div className="ml-auto flex items-center gap-1.5">
                    <span className="relative flex size-2">
                        {isConnected && (
                            <span className="absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75 animate-ping" />
                        )}
                        <span
                            className={`relative inline-flex rounded-full size-2 ${
                                isConnected ? "bg-emerald-500" : "bg-slate-300"
                            }`}
                        />
                    </span>
                    <span className="text-[11px] text-slate-500 tabular-nums">{connectionLabel}</span>
                </div>
            </div>

            {/* ── Now sending ────────────────────────────────────────── */}
            {showNowSending && (
                <div className="shrink-0 px-4 py-3 border-b border-slate-200 bg-sky-50/40">
                    <div className="flex items-center gap-3">
                        <div className="size-9 shrink-0 rounded-full bg-sky-100 text-sky-700 flex items-center justify-center font-medium text-[12.5px]">
                            {initials(taskProgress.contact_name, taskProgress.contact_email)}
                        </div>
                        <div className="flex-1 min-w-0">
                            <p className="text-[12.5px] font-medium text-slate-900 truncate">
                                {taskProgress.contact_name || taskProgress.contact_email || "Unknown contact"}
                            </p>
                            {taskProgress.contact_name && (
                                <p className="text-[11px] text-slate-500 truncate">
                                    {taskProgress.contact_email}
                                </p>
                            )}
                            {taskProgress.step_name && (
                                <p className="text-[11px] text-sky-700 truncate mt-0.5">
                                    {taskProgress.step_name}
                                    {taskProgress.step_index > 0 && ` · Step ${taskProgress.step_index}`}
                                </p>
                            )}
                        </div>
                        <span className="shrink-0 inline-flex items-center gap-1.5 px-1.5 h-5 rounded-md bg-sky-50 text-sky-700 ring-1 ring-sky-200 text-[10.5px] font-medium">
                            <span className="size-1.5 rounded-full bg-sky-500 animate-pulse" />
                            Sending…
                        </span>
                    </div>
                </div>
            )}

            {/* ── Progress ───────────────────────────────────────────── */}
            {(showNowSending || (isActive && total > 0)) && (
                <div className="shrink-0 px-4 py-3 border-b border-slate-200">
                    <div className="flex items-center justify-between mb-1.5">
                        <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                            Progress
                        </span>
                        <span className="font-mono text-[11px] text-slate-700 tabular-nums">{progress}%</span>
                    </div>
                    <div className="h-1.5 rounded-full bg-slate-100 overflow-hidden">
                        <div
                            className="h-full rounded-full bg-sky-600 transition-all duration-500"
                            style={{ width: `${progress}%` }}
                        />
                    </div>
                    <div className="flex items-center justify-between mt-1.5">
                        <span className="font-mono text-[10.5px] text-slate-400 tabular-nums">
                            {processed.toLocaleString()} of {total.toLocaleString()} contacts
                        </span>
                        {remainingHint && (
                            <span className="text-[10.5px] text-slate-400">{remainingHint}</span>
                        )}
                    </div>
                </div>
            )}

            {/* ── Issues (failures) — always visible ─────────────────── */}
            {issueLogs.length > 0 && (
                <div className="shrink-0 border-b border-rose-200 bg-rose-50/60">
                    <div className="px-4 pt-2.5 pb-1 text-[10px] uppercase tracking-[0.14em] text-rose-500 font-medium">
                        Needs attention
                    </div>
                    <div className="divide-y divide-rose-200/50">
                        {issueLogs.map((log, i) => (
                            <div key={i} className="flex items-start gap-2.5 px-4 py-2">
                                <span className="size-1.5 mt-1.5 shrink-0 rounded-full bg-rose-500" />
                                <p className="flex-1 min-w-0 text-[12px] text-rose-700 leading-snug break-words">
                                    {log.message}
                                </p>
                                <span className="shrink-0 font-mono text-[10.5px] text-rose-400 tabular-nums mt-0.5">
                                    {relativeTime(new Date(log.timestamp))}
                                </span>
                            </div>
                        ))}
                    </div>
                </div>
            )}

            {/* ── Activity feed ──────────────────────────────────────── */}
            <div className="flex-1 min-h-0 max-h-72 overflow-auto">
                {hasActivity ? (
                    <div className="divide-y divide-slate-200/60">
                        {activities.map((activity) => (
                            <ActivityRow key={activity.id} activity={activity} />
                        ))}
                    </div>
                ) : recentLogs.length > 0 ? (
                    <>
                        <div className="px-4 pt-3 pb-1 text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                            Recent log
                        </div>
                        <div className="divide-y divide-slate-200/60">
                            {recentLogs.map((log, i) => (
                                <div key={i} className="flex items-start gap-2.5 px-4 py-2">
                                    <span
                                        className={`size-1.5 mt-1.5 shrink-0 rounded-full ${
                                            log.level === "error"
                                                ? "bg-rose-500"
                                                : log.level === "warn"
                                                  ? "bg-amber-500"
                                                  : "bg-slate-300"
                                        }`}
                                    />
                                    <p className="flex-1 min-w-0 text-[12px] text-slate-600 leading-snug break-words">
                                        {log.message}
                                    </p>
                                    <span className="shrink-0 font-mono text-[10.5px] text-slate-400 tabular-nums mt-0.5">
                                        {relativeTime(new Date(log.timestamp))}
                                    </span>
                                </div>
                            ))}
                        </div>
                    </>
                ) : (
                    <div className="px-5 py-16 text-center">
                        <p className="text-[12.5px] text-slate-700 font-medium mb-1">
                            {isActive ? "Waiting for the next send…" : "Nothing sending yet"}
                        </p>
                        <p className="text-[11.5px] text-slate-400 max-w-[34ch] mx-auto leading-relaxed">
                            {isActive
                                ? "Opens, clicks, replies and bounces will stream in here live as your campaign sends."
                                : "Start the campaign to watch it send live."}
                        </p>
                    </div>
                )}
            </div>
        </div>
    );
}
