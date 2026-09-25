// A lead's Pause and Resume actions, shared by the contact drawer and the unibox contact panel.

import React from "react";
import { Loader2Icon, PauseIcon, PlayIcon } from "lucide-react";
import toast from "react-hot-toast";
import PauseLeadDialog from "./PauseLeadDialog";
import { useResumeLead } from "@/lib/api/hooks/app/campaigns/useLeadHold";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

export function PauseLeadButton({
    campaign,
    lead,
    label = "Pause",
}: {
    campaign: { id: string; name: string };
    lead: { id: string; name: string };
    label?: string;
}) {
    const [open, setOpen] = React.useState(false);
    return (
        <>
            <button
                type="button"
                onClick={() => setOpen(true)}
                title="Hold this lead's follow-ups until a date, without unsubscribing them"
                className="h-6 px-2 rounded-md border border-slate-200 bg-white hover:border-slate-300 text-[11px] text-slate-600 hover:text-slate-900 inline-flex items-center gap-1 transition-colors shrink-0"
            >
                <PauseIcon className="w-2.5 h-2.5" />
                {label}
            </button>
            <PauseLeadDialog open={open} onClose={() => setOpen(false)} campaign={campaign} lead={open ? lead : null} />
        </>
    );
}

export function ResumeLeadButton({
    campaignId,
    contactId,
    label = "Resume",
    disabled = false,
    onBusyChange,
}: {
    campaignId: string;
    contactId: string;
    label?: string;
    disabled?: boolean;
    onBusyChange?: (busy: boolean) => void;
}) {
    const resume = useResumeLead();
    async function run() {
        onBusyChange?.(true);
        try {
            await toast.promise(resume.mutateAsync({ campaignId, contactId }), {
                loading: "Resuming lead…",
                success: "Lead resumed",
                error: (err: AppError) => buildError(err),
            });
        } catch {
            /* toast.promise already surfaced it */
        } finally {
            onBusyChange?.(false);
        }
    }
    return (
        <button
            type="button"
            onClick={run}
            disabled={disabled || resume.isPending}
            title="Lift the pause now; the flow picks up where it stopped"
            className="h-6 px-2 rounded-md bg-white border border-violet-200 text-[11px] font-medium text-violet-700 hover:bg-violet-100 inline-flex items-center gap-1 transition-colors disabled:opacity-60 shrink-0"
        >
            {resume.isPending ? <Loader2Icon className="w-2.5 h-2.5 animate-spin" /> : <PlayIcon className="w-2.5 h-2.5" />}
            {label}
        </button>
    );
}
