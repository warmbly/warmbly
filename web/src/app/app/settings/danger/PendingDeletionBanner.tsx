// Banner shown in the danger zone when a deletion is already scheduled.
//
// Two jobs: tell the user what's about to disappear and when, and give
// them a one-click escape hatch. The "Time remaining" ticks every
// minute so a long-open tab still shows fresh numbers.

import React from "react";
import { AlertOctagonIcon, Loader2Icon, UndoIcon } from "lucide-react";
import toast from "react-hot-toast";
import buildError from "@/lib/helper/buildError";
import type { AppError } from "@/lib/api/client/normalizeError";
import type ScheduledDeletion from "@/lib/api/models/app/dangerzone/ScheduledDeletion";

interface Props {
    title: string;
    deletion: ScheduledDeletion;
    onCancel: () => Promise<unknown>;
    cancelLabel?: string;
}

function formatRemaining(executeAfter: Date): string {
    const ms = executeAfter.getTime() - Date.now();
    if (ms <= 0) return "any moment now";
    const totalMinutes = Math.floor(ms / 60_000);
    const days = Math.floor(totalMinutes / (60 * 24));
    const hours = Math.floor((totalMinutes % (60 * 24)) / 60);
    const minutes = totalMinutes % 60;
    if (days > 0) return `${days}d ${hours}h`;
    if (hours > 0) return `${hours}h ${minutes}m`;
    return `${minutes}m`;
}

function formatAbsolute(d: Date): string {
    return d.toLocaleString(undefined, {
        weekday: "short",
        day: "numeric",
        month: "short",
        year: "numeric",
        hour: "2-digit",
        minute: "2-digit",
    });
}

export default function PendingDeletionBanner({
    title,
    deletion,
    onCancel,
    cancelLabel = "Cancel deletion",
}: Props) {
    const [loading, setLoading] = React.useState(false);
    // Re-render every minute so the "time remaining" stays honest on
    // long-lived tabs without us pumping an extra fetch.
    const [, setTick] = React.useState(0);
    React.useEffect(() => {
        const id = window.setInterval(() => setTick((t) => t + 1), 60_000);
        return () => window.clearInterval(id);
    }, []);

    const executeAfter = new Date(deletion.execute_after);

    const handleCancel = async () => {
        if (loading) return;
        try {
            setLoading(true);
            await onCancel();
            toast.success("Deletion cancelled");
        } catch (err) {
            toast.error(buildError(err as AppError));
        } finally {
            setLoading(false);
        }
    };

    return (
        <div className="rounded-md border border-red-300 bg-red-50/70 overflow-hidden">
            <div className="px-3.5 py-3 border-b border-red-200/70 flex items-start gap-3">
                <div className="size-6 rounded bg-red-100 text-red-700 flex items-center justify-center shrink-0">
                    <AlertOctagonIcon className="w-3.5 h-3.5" />
                </div>
                <div className="min-w-0 flex-1">
                    <div className="text-[13px] font-semibold text-red-800 leading-tight">
                        {title}
                    </div>
                    <div className="text-[11.5px] text-red-700/80 leading-tight mt-0.5">
                        Scheduled by you on{" "}
                        {new Date(deletion.scheduled_at).toLocaleDateString()}.
                        {deletion.reason ? ` Reason: "${deletion.reason}".` : ""}
                    </div>
                </div>
            </div>

            <div className="px-3.5 py-3 grid grid-cols-1 sm:grid-cols-2 gap-3">
                <DetailCell
                    label="Permanent delete on"
                    value={formatAbsolute(executeAfter)}
                />
                <DetailCell
                    label="Time remaining"
                    value={formatRemaining(executeAfter)}
                />
            </div>

            <div className="px-3 h-11 border-t border-red-200/70 bg-red-100/30 flex items-center gap-1.5">
                <span className="text-[11.5px] text-red-700/80">
                    Cancel any time before then to keep everything intact.
                </span>
                <button
                    type="button"
                    onClick={handleCancel}
                    disabled={loading}
                    className="ml-auto h-7 px-2.5 rounded-md bg-white border border-red-300 hover:border-red-400 text-red-700 hover:text-red-800 hover:bg-red-50 text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                >
                    {loading ? (
                        <Loader2Icon className="w-3 h-3 animate-spin" />
                    ) : (
                        <UndoIcon className="w-3 h-3" />
                    )}
                    {cancelLabel}
                </button>
            </div>
        </div>
    );
}

function DetailCell({ label, value }: { label: string; value: string }) {
    return (
        <div>
            <div className="text-[10px] uppercase tracking-[0.1em] text-red-700/70 font-medium">
                {label}
            </div>
            <div className="text-[13px] text-red-900 font-semibold leading-tight mt-0.5">
                {value}
            </div>
        </div>
    );
}
