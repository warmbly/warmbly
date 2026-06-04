import React from "react";
import { LayersIcon, Loader2Icon, PlusIcon } from "lucide-react";
import toast from "react-hot-toast";
import { useCampaign } from "@/hooks/context/campaign";
import StepRail from "@/components/app/campaigns/sequences/StepRail";
import SequenceView from "@/components/app/campaigns/sequences/SequenceView";
import useSequences from "@/lib/api/hooks/app/campaigns/sequences/useSequences";
import useCreateSequence from "@/lib/api/hooks/app/campaigns/sequences/useCreateSequence";
import type Sequence from "@/lib/api/models/app/campaigns/sequences/Sequence";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

export default function CampaignSequences() {
    const campaign = useCampaign();
    if (!campaign) {
        throw new Error("CampaignSequences cannot be rendered without a campaign");
    }

    return (
        <React.Suspense fallback={<SequencesSkeleton />}>
            <SequencesBuilder campaignId={campaign.id} />
        </React.Suspense>
    );
}

function SequencesBuilder({ campaignId }: { campaignId: string }) {
    const { data: sequences } = useSequences(campaignId);
    const createSequence = useCreateSequence(campaignId);
    const [creating, setCreating] = React.useState(false);
    const [selectedId, setSelectedId] = React.useState<string>("");

    // Keep a valid selection: default to the first step, and recover if the
    // selected step is deleted.
    React.useEffect(() => {
        if (sequences.length === 0) {
            if (selectedId !== "") setSelectedId("");
            return;
        }
        if (!sequences.some((s) => s.id === selectedId)) {
            setSelectedId(sequences[0].id);
        }
    }, [sequences, selectedId]);

    async function create() {
        if (creating) return;
        setCreating(true);
        try {
            const created = await toast.promise(createSequence.mutateAsync(), {
                loading: "Adding step…",
                success: "Step added.",
                error: (err: AppError) => buildError(err),
            });
            setSelectedId((created as Sequence).id);
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
                <h2 className="text-[13px] font-medium text-slate-900">Build your sequence</h2>
                <p className="mt-1 mb-4 max-w-xs text-center text-[11.5px] leading-relaxed text-slate-400">
                    Add steps to automate the outreach flow. The first step sends immediately;
                    later steps wait and thread as follow-ups.
                </p>
                <button
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
                </button>
            </div>
        );
    }

    const selected = sequences.find((s) => s.id === selectedId);
    const selectedIndex = sequences.findIndex((s) => s.id === selectedId);

    return (
        <div className="flex flex-col gap-4 md:flex-row">
            <div className="shrink-0 md:w-60 xl:w-72">
                <div className="mb-2 text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                    Steps
                </div>
                <StepRail
                    campaignId={campaignId}
                    sequences={sequences}
                    selectedId={selectedId}
                    onSelect={setSelectedId}
                    onCreate={create}
                    creating={creating}
                />
            </div>
            <div className="min-w-0 flex-1">
                {selected ? (
                    <SequenceView
                        key={selected.id}
                        campaignId={campaignId}
                        sequence={selected}
                        index={selectedIndex}
                    />
                ) : (
                    <div className="rounded-md border border-slate-200 bg-white px-5 py-16 text-center">
                        <p className="text-[12.5px] font-medium text-slate-700">Select a step</p>
                        <p className="mt-1 text-[11.5px] text-slate-400">
                            Pick a step from the list to edit its email.
                        </p>
                    </div>
                )}
            </div>
        </div>
    );
}

function SequencesSkeleton() {
    return (
        <div className="flex flex-col gap-4 md:flex-row">
            <div className="shrink-0 space-y-2 md:w-60 xl:w-72">
                {[...Array(3)].map((_, i) => (
                    <div key={i} className="h-14 animate-pulse rounded-md bg-slate-100" />
                ))}
            </div>
            <div className="min-w-0 flex-1">
                <div className="h-80 animate-pulse rounded-md bg-slate-100" />
            </div>
        </div>
    );
}
