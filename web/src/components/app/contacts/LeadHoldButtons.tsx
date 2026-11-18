// A lead's Pause and Resume actions, shared by the contact drawer and the unibox contact panel.

import React from "react";
import { Loader2Icon, PauseIcon, PlayIcon } from "lucide-react";
import toast from "react-hot-toast/headless";
import PauseLeadDialog from "./PauseLeadDialog";
import { useResumeLead } from "@/lib/api/hooks/app/campaigns/useLeadHold";
import { useApproveLeads } from "@/lib/api/hooks/app/campaigns/useCampaignReview";
import { useConfirm } from "@/hooks/context/confirm";
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
    confirmText,
    approve = false,
}: {
    campaignId: string;
    contactId: string;
    label?: string;
    disabled?: boolean;
    onBusyChange?: (busy: boolean) => void;
    // Asked first when resuming has a consequence worth a second look.
    confirmText?: string;
    // The lead waits for review, which only an approval lifts.
    approve?: boolean;
}) {
    const resume = useResumeLead();
    const approveLeads = useApproveLeads(campaignId);
    const confirm = useConfirm();
    async function run() {
        onBusyChange?.(true);
        try {
            await toast.promise(
                (async () => {
                    if (approve) return (await approveLeads.mutateAsync({ contact_ids: [contactId] })).released;
                    await resume.mutateAsync({ campaignId, contactId });
                    return 1;
                })(),
                {
                    loading: approve ? "Approving lead…" : "Resuming lead…",
                    // Zero released: the hold changed before the approval landed; the refresh shows what it is now.
                    success: (released) =>
                        !approve ? "Lead resumed" : released > 0 ? "Approved. Their first email goes out on schedule." : "This lead was no longer waiting for approval.",
                    error: (err: AppError) => buildError(err),
                },
            );
        } catch {
            /* toast.promise already surfaced it */
        } finally {
            onBusyChange?.(false);
        }
    }
    return (
        <button
            type="button"
            onClick={() => (confirmText ? confirm.show(confirmText, run) : void run())}
            disabled={disabled || resume.isPending || approveLeads.isPending}
            title={approve ? "Approve this lead; their first email goes out on schedule" : "Lift the pause now; the flow picks up where it stopped"}
            className="h-6 px-2 rounded-md bg-white border border-violet-200 text-[11px] font-medium text-violet-700 hover:bg-violet-100 inline-flex items-center gap-1 transition-colors disabled:opacity-60 shrink-0"
        >
            {resume.isPending || approveLeads.isPending ? <Loader2Icon className="w-2.5 h-2.5 animate-spin" /> : <PlayIcon className="w-2.5 h-2.5" />}
            {label}
        </button>
    );
}
