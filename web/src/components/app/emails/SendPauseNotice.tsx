import { Link } from "@tanstack/react-router";
import toast from "react-hot-toast/headless";
import { GlobeIcon, PauseCircleIcon, PlayIcon } from "lucide-react";
import type React from "react";
import type { SendPause } from "@/lib/api/models/app/analytics/AccountStatus";
import useResumeSending from "@/lib/api/hooks/app/emails/useResumeSending";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { usePermission } from "@/hooks/usePermission";
import { relativeTime } from "@/lib/unibox/messageDetails";
import { sendPauseCopy, sendPauseResumesAt } from "@/lib/sendPause";

// The drawer's account of a provider pause: why, until when, the provider's words and the one action that helps.
export default function SendPauseNotice({ mailboxId, address, pause, reconnect }: { mailboxId: string; address: string; pause: SendPause; reconnect?: React.ReactNode }) {
    const copy = sendPauseCopy(pause);
    const canManage = usePermission("MANAGE_EMAILS");
    const resume = useResumeSending(mailboxId);
    const at = sendPauseResumesAt(pause);
    const domain = address.split("@")[1]?.toLowerCase();
    const onResume = () =>
        resume.mutate(undefined, {
            onSuccess: (r) => toast.success(r.hold_released || r.cooldown_cleared ? "Sending resumed" : "Sending was not paused anymore"),
            onError: (e) => toast.error(buildError(e as unknown as AppError)),
        });

    return (
        <div className="px-5 py-4">
            <div className="rounded-md border border-amber-200 bg-amber-50 px-3 py-2.5 flex gap-2.5">
                <PauseCircleIcon className="w-4 h-4 text-amber-600 shrink-0 mt-0.5" />
                <div className="min-w-0 flex-1">
                    <p className="text-[12.5px] font-medium text-amber-900">{copy.title}</p>
                    {at && (
                        <p className="text-[11.5px] text-amber-900/90 mt-0.5" title={at.toLocaleString()}>
                            {pause.held ? "Next automatic try " : "Paused until "}
                            <span className="font-medium">{at.toLocaleString()}</span> ({relativeTime(at)})
                        </p>
                    )}
                    <p className="text-[11.5px] text-amber-800/90 leading-relaxed mt-1">{copy.body}</p>
                    {pause.provider_message && (
                        <p className="mt-1.5 text-[11px] text-amber-900/80 font-mono break-words">
                            <span className="sr-only">The provider said: </span>
                            {pause.provider_message}
                        </p>
                    )}
                    {(copy.action === "fix_dns" || (pause.resumable && canManage)) && (
                        <div className="mt-2 flex flex-wrap items-center gap-2">
                            {copy.action === "fix_dns" && domain && (
                                <Link
                                    to="/app/emails/domains"
                                    search={{ domain }}
                                    className="h-7 px-2.5 inline-flex items-center gap-1.5 rounded-md bg-amber-600 text-white text-[12px] font-medium hover:bg-amber-700"
                                >
                                    <GlobeIcon className="w-3.5 h-3.5" /> Fix DNS for {domain}
                                </Link>
                            )}
                            {pause.resumable && canManage && (
                                <button
                                    type="button"
                                    onClick={onResume}
                                    disabled={resume.isPending}
                                    className="h-7 px-2.5 inline-flex items-center gap-1.5 rounded-md border border-amber-300 bg-white text-amber-800 text-[12px] font-medium hover:bg-amber-100 disabled:opacity-60"
                                >
                                    <PlayIcon className="w-3.5 h-3.5" /> {resume.isPending ? "Resuming…" : "Resume now"}
                                </button>
                            )}
                        </div>
                    )}
                    {copy.action === "reconnect" && reconnect}
                </div>
            </div>
        </div>
    );
}
