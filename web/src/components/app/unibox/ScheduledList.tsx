// Scheduled-sends view. Replaces the conversation list when scope =
// "scheduled". Each row shows what's queued (from / to / when / subject
// / preview) and lets the user cancel the send. Cancel is DB-only —
// the server flips status to 'cancelled' and the queued Cloud Task
// fires as a no-op, so we avoid paying Cloud Tasks per cancel.
//
// Lists are short by design (queued sends are an exception, not a
// firehose), so we don't paginate; the server caps at 200.

import React from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import toast from "react-hot-toast";
import {
    AlertCircleIcon,
    ClockIcon,
    InboxIcon,
    Loader2Icon,
    SendIcon,
    XIcon,
} from "lucide-react";
import useUniboxScheduled from "@/lib/api/hooks/app/unibox/useUniboxScheduled";
import cancelScheduled from "@/lib/api/client/app/unibox/cancelScheduled";
import type UniboxScheduledItem from "@/lib/api/models/app/unibox/UniboxScheduled";
import { cn } from "@/lib/utils";

function formatWhen(iso: string): { absolute: string; relative: string } {
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return { absolute: "—", relative: "" };

    const now = Date.now();
    const diffMs = d.getTime() - now;
    const absMs = Math.abs(diffMs);
    const future = diffMs > 0;
    const minutes = Math.round(absMs / 60_000);
    const hours = Math.round(absMs / 3_600_000);
    const days = Math.round(absMs / 86_400_000);

    let relative: string;
    if (absMs < 60_000) relative = future ? "in a moment" : "any moment";
    else if (minutes < 60) relative = future ? `in ${minutes}m` : `${minutes}m late`;
    else if (hours < 24) relative = future ? `in ${hours}h` : `${hours}h late`;
    else relative = future ? `in ${days}d` : `${days}d late`;

    const absolute = d.toLocaleString(undefined, {
        weekday: "short",
        month: "short",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
    });

    return { absolute, relative };
}

export function ScheduledList() {
    const q = useUniboxScheduled();
    const queryClient = useQueryClient();
    const [cancelingId, setCancelingId] = React.useState<string | null>(null);

    const cancel = useMutation({
        mutationFn: (taskId: string) => cancelScheduled(taskId),
        onMutate: (taskId: string) => setCancelingId(taskId),
        onSuccess: () => {
            toast.success("Scheduled send cancelled");
            queryClient.invalidateQueries({ queryKey: ["unibox", "scheduled"] });
            queryClient.invalidateQueries({ queryKey: ["unibox", "overview"] });
        },
        onError: () => toast.error("Couldn't cancel — it may have already sent."),
        onSettled: () => setCancelingId(null),
    });

    if (q.isPending) {
        return (
            <div className="flex-1 flex items-center justify-center gap-2 text-[12px] text-slate-400">
                <Loader2Icon className="w-3.5 h-3.5 animate-spin" />
                Loading scheduled sends…
            </div>
        );
    }

    if (q.isError) {
        return (
            <div className="flex-1 flex items-center justify-center px-6">
                <div className="text-center max-w-sm">
                    <AlertCircleIcon className="w-5 h-5 text-rose-500 mx-auto mb-2" />
                    <p className="text-[12.5px] font-medium text-slate-900 mb-1">
                        Couldn't load scheduled sends
                    </p>
                    <button
                        type="button"
                        onClick={() => q.refetch()}
                        className="h-7 px-2.5 mt-2 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium transition-colors"
                    >
                        Try again
                    </button>
                </div>
            </div>
        );
    }

    const items = q.data?.data ?? [];

    if (items.length === 0) {
        return (
            <div className="flex-1 flex items-center justify-center">
                <div className="text-center px-5">
                    <div className="w-8 h-8 rounded-md bg-slate-100 flex items-center justify-center mx-auto mb-3 text-slate-400">
                        <SendIcon className="w-4 h-4" />
                    </div>
                    <p className="text-[12.5px] font-medium text-slate-700">
                        No scheduled sends
                    </p>
                    <p className="text-[11.5px] text-slate-400 mt-1 max-w-[34ch] leading-relaxed">
                        Replies you schedule with the picker show up here until they fire.
                    </p>
                </div>
            </div>
        );
    }

    return (
        <div className="flex-1 flex flex-col min-h-0">
            <div className="h-10 px-5 border-b border-slate-200 flex items-center gap-3 shrink-0">
                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                    Scheduled
                </span>
                <span className="text-[11.5px] text-slate-500">
                    {items.length} pending
                </span>
                <button
                    type="button"
                    onClick={() => q.refetch()}
                    className="ml-auto h-6 px-2 rounded text-[11px] text-slate-500 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                >
                    Refresh
                </button>
            </div>

            <div className="flex-1 overflow-y-auto">
                <ul className="divide-y divide-slate-200/70">
                    {items.map((item) => (
                        <ScheduledRow
                            key={item.task_id}
                            item={item}
                            disabled={cancelingId === item.task_id || cancel.isPending}
                            isPending={cancelingId === item.task_id}
                            onCancel={() => cancel.mutate(item.task_id)}
                        />
                    ))}
                </ul>

                <div className="px-5 py-4 text-[11px] text-slate-400 leading-relaxed border-t border-slate-100">
                    <InboxIcon className="inline w-3 h-3 mr-1 -mt-px" />
                    Cancelling a send keeps the body and recipients on record — only the
                    delivery is stopped. If a queued send fires after you cancel, the
                    server skips it silently.
                </div>
            </div>
        </div>
    );
}

function ScheduledRow({
    item,
    disabled,
    isPending,
    onCancel,
}: {
    item: UniboxScheduledItem;
    disabled: boolean;
    isPending: boolean;
    onCancel: () => void;
}) {
    const when = formatWhen(item.scheduled_at);
    const recipients = item.to.join(", ");
    const ccCount = (item.cc?.length ?? 0) + (item.bcc?.length ?? 0);
    const overdue = new Date(item.scheduled_at).getTime() < Date.now();

    return (
        <li className="px-5 py-3 group hover:bg-slate-50/70 transition-colors">
            <div className="flex items-start gap-3">
                <div
                    className={cn(
                        "shrink-0 size-7 rounded-md flex items-center justify-center",
                        overdue
                            ? "bg-amber-50 text-amber-700 ring-1 ring-amber-200"
                            : "bg-sky-50 text-sky-700 ring-1 ring-sky-100",
                    )}
                    aria-hidden
                >
                    <ClockIcon className="w-3.5 h-3.5" />
                </div>

                <div className="flex-1 min-w-0">
                    <div className="flex items-baseline gap-2 mb-0.5">
                        <span
                            className={cn(
                                "font-mono text-[11px] tabular-nums px-1.5 h-4 inline-flex items-center rounded",
                                overdue
                                    ? "bg-amber-100 text-amber-800"
                                    : "bg-sky-100 text-sky-700",
                            )}
                            title={item.scheduled_at}
                        >
                            {when.relative || "soon"}
                        </span>
                        <span className="text-[11px] text-slate-500 truncate">
                            {when.absolute}
                        </span>
                    </div>

                    <p className="text-[13px] font-medium text-slate-900 truncate">
                        {item.subject || "(no subject)"}
                    </p>

                    <p className="mt-0.5 text-[11.5px] text-slate-500 truncate">
                        <span className="text-slate-400">to</span> {recipients}
                        {ccCount > 0 && (
                            <span className="text-slate-400"> · +{ccCount} cc/bcc</span>
                        )}
                    </p>

                    {item.snippet && (
                        <p className="mt-1 text-[11.5px] text-slate-500 line-clamp-2">
                            {item.snippet}
                        </p>
                    )}

                    <div className="mt-1.5 flex items-center gap-2">
                        <span className="text-[10.5px] font-mono text-slate-500 bg-slate-100 px-1.5 h-4 inline-flex items-center rounded">
                            {item.account_email}
                        </span>
                        {item.thread_id && (
                            <span className="text-[10.5px] text-slate-400">
                                replies into thread
                            </span>
                        )}
                    </div>
                </div>

                <button
                    type="button"
                    onClick={onCancel}
                    disabled={disabled}
                    className={cn(
                        "shrink-0 h-7 px-2 rounded-md text-[11.5px] font-medium inline-flex items-center gap-1 transition-colors",
                        "border border-slate-200 text-slate-600",
                        "opacity-100 md:opacity-0 md:group-hover:opacity-100",
                        "hover:border-rose-300 hover:bg-rose-50 hover:text-rose-700",
                        "disabled:opacity-40 disabled:cursor-not-allowed",
                    )}
                >
                    {isPending ? (
                        <Loader2Icon className="w-3 h-3 animate-spin" />
                    ) : (
                        <XIcon className="w-3 h-3" />
                    )}
                    {isPending ? "Cancelling…" : "Cancel"}
                </button>
            </div>
        </li>
    );
}
