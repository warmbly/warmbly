import React from "react";
import { LayersIcon, Loader2Icon, PlusIcon } from "lucide-react";
import toast from "react-hot-toast";
import { useCampaign } from "@/hooks/context/campaign";
import CampaignFlow from "@/components/app/campaigns/sequences/CampaignFlow";
import PermissionButton from "@/components/ui/PermissionButton";
import useSequences from "@/lib/api/hooks/app/campaigns/sequences/useSequences";
import useCreateSequence from "@/lib/api/hooks/app/campaigns/sequences/useCreateSequence";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

export default function CampaignSteps() {
    const campaign = useCampaign();
    if (!campaign) {
        throw new Error("CampaignSteps cannot be rendered without a campaign");
    }

    return (
        <React.Suspense fallback={<StepsSkeleton />}>
            <StepsBuilder campaignId={campaign.id} />
        </React.Suspense>
    );
}

function StepsBuilder({ campaignId }: { campaignId: string }) {
    const { data: sequences } = useSequences(campaignId);
    const createSequence = useCreateSequence(campaignId);
    const [creating, setCreating] = React.useState(false);

    async function create() {
        if (creating) return;
        setCreating(true);
        try {
            await toast.promise(createSequence.mutateAsync(), {
                loading: "Adding step…",
                success: "Step added.",
                error: (err: AppError) => buildError(err),
            });
        } finally {
            setCreating(false);
        }
    }

    if (sequences.length === 0) {
        return (
            <div className="flex flex-col items-center justify-center rounded-md border border-slate-200 bg-white py-16">
                <div className="mb-3 flex size-10 items-center justify-center rounded-md bg-sky-50 text-sky-600">
                    <LayersIcon className="w-4 h-4" />
                </div>
                <h2 className="text-[13px] font-medium text-slate-900">Build your flow</h2>
                <p className="mt-1 mb-4 max-w-xs text-center text-[11.5px] leading-relaxed text-slate-400">
                    Add your first step, then drag from a step to branch on opens, clicks, or
                    replies. The first email sends immediately; later steps wait and thread as
                    follow-ups.
                </p>
                <PermissionButton
                    permission="MANAGE_CAMPAIGNS"
                    type="button"
                    onClick={create}
                    disabled={creating}
                    className="inline-flex h-7 items-center gap-1.5 rounded-md bg-sky-600 px-3 text-[12px] font-medium text-white transition-colors hover:bg-sky-700 disabled:opacity-60"
                >
                    {creating ? (
                        <Loader2Icon className="w-3.5 h-3.5 animate-spin" />
                    ) : (
                        <PlusIcon className="w-3.5 h-3.5" />
                    )}
                    Add your first step
                </PermissionButton>
            </div>
        );
    }

    // Keyed by campaign: a param-only navigation (e.g. jump-to-teammate from
    // one campaign's steps to another's) must remount the canvas, never reuse
    // one seeded from the previous campaign.
    return <CampaignFlow key={campaignId} campaignId={campaignId} />;
}

function StepsSkeleton() {
    return <div className="h-[74dvh] w-full animate-pulse rounded-md border border-slate-200 bg-slate-100/60" />;
}
