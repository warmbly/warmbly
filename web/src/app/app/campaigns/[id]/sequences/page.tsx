import React from "react";
import { useCampaign } from "@/hooks/context/campaign";
import { Loading } from "@/components/loader";
import SequenceBox from "@/components/app/campaigns/sequences/SequenceBox";
import SequenceView from "@/components/app/campaigns/sequences/SequenceView";
import useSequences from "@/lib/api/hooks/app/campaigns/sequences/useSequences";
import useCreateSequence from "@/lib/api/hooks/app/campaigns/sequences/useCreateSequence";
import toast from "react-hot-toast";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import type Sequence from "@/lib/api/models/app/campaigns/sequences/Sequence";
import { FileTextIcon, PlusIcon } from "lucide-react";

export default function CampaignSequences() {
    const campaign = useCampaign();
    if (!campaign) {
        throw new Error("CampaignSequences cannot be rendered without a campaign")
    }

    const [load, setLoad] = React.useState<boolean>(false);
    const [select, setSelect] = React.useState<string>("");
    const [newSequences, setNewSequences] = React.useState<Sequence[] | null>()
    const sequencesData = useSequences(campaign.id);

    const createSequence = useCreateSequence(campaign.id);

    async function CreateSequence() {
        if (load) return;
        setLoad(true);
        try {
            const resp = await toast.promise(
                createSequence.mutateAsync,
                {
                    loading: "Creating sequence...",
                    success: "Sequence successfully created.",
                    error: (err: AppError) => buildError(err),
                }
            )
            setNewSequences(bef => bef ? [...bef, resp] : [resp])
        } finally {
            setLoad(false);
        }
    }

    return !sequencesData.isLoading ? (
        <div className="flex flex-col md:flex-row gap-4">
            <div className="md:w-56 xl:w-64 shrink-0 space-y-2">
                {sequencesData.data.map((seq, i) => {
                    if (!campaign.sequences || !newSequences || campaign.sequences.length !== newSequences.length) return null;
                    return (
                        <SequenceBox
                            key={seq.id}
                            next={i !== campaign.sequences.length - 1}
                            active={seq.id === select}
                            def_wait={seq.wait_after}
                            wait={newSequences[i].wait_after}
                            setWait={(v) => setNewSequences(bef => bef ? bef.map((s) => s.id === seq.id ? {
                                ...s,
                                wait_after: v,
                            } : s) : null)}
                            onClick={() => setSelect(seq.id)}
                        >{seq.name}</SequenceBox>
                    )
                })}
                {sequencesData.data.length === 0 && (
                    <SequenceBox
                        next={false}
                        active
                        def_wait={10}
                        wait={10}
                        setWait={() => { }}
                        onClick={() => { }}
                    >New Sequence</SequenceBox>
                )}
                {sequencesData.data.length < 5 && (
                    <button
                        className={`flex items-center justify-center gap-1.5 w-full rounded-lg text-[13px] font-medium transition-colors duration-100 px-3 py-1.5 ${load ? "bg-zinc-100 text-zinc-400" : "text-zinc-500 hover:text-zinc-900 border border-zinc-200 rounded-lg cursor-pointer"}`}
                        onClick={CreateSequence}
                    >
                        {load ? <Loading className="h-4" /> : <><PlusIcon className="w-3.5 h-3.5" />New Sequence</>}
                    </button>
                )}
            </div>
            <div className="flex-1 min-w-0">
                {(() => {
                    const seq = sequencesData.data.find((v) => v.id === select)
                    const seq2 = newSequences?.find((v) => v.id === select)
                    if (!seq || !seq2 || !campaign.sequences) {
                        return (
                            <div className="flex flex-col items-center justify-center py-16 bg-white rounded-xl border border-zinc-200">
                                <div className="w-10 h-10 rounded-xl bg-zinc-100 flex items-center justify-center mb-3">
                                    <FileTextIcon className="w-4 h-4 text-zinc-400" />
                                </div>
                                <h2 className="text-sm font-medium text-zinc-900 mb-1">Create your first sequence</h2>
                                <p className="text-xs text-zinc-400 text-center max-w-xs mb-4">
                                    Add email sequences to automate your outreach flow.
                                </p>
                                <button
                                    className="bg-zinc-900 text-white hover:bg-zinc-800 rounded-lg px-3 py-1.5 text-[13px] font-medium transition-colors duration-100 flex items-center gap-1.5"
                                    onClick={CreateSequence}
                                >
                                    {load ? <Loading className="h-4" /> : <><PlusIcon className="w-3.5 h-3.5" />Create Sequence</>}
                                </button>
                            </div>
                        )
                    }
                    return <SequenceView
                        campaign_id={campaign.id}
                        def_sequence={seq}
                        sequence={seq2}
                        setName={(v) => setNewSequences(bef => bef ? bef.map((s) => s.id === select ? {
                            ...s,
                            name: v,
                        } : s) : null)}
                        setSubject={(v) => setNewSequences(bef => bef ? bef.map((s) => s.id === select ? {
                            ...s,
                            subject: v,
                        } : s) : null)}
                        setBodyPlain={(v) => setNewSequences(bef => bef ? bef.map((s) => s.id === select ? {
                            ...s,
                            body_plain: v,
                        } : s) : null)}
                        setBodyHTML={(v) => setNewSequences(bef => bef ? bef.map((s) => s.id === select ? {
                            ...s,
                            body_html: v,
                        } : s) : null)}
                        setBodySync={(v) => setNewSequences(bef => bef ? bef.map((s) => s.id === select ? {
                            ...s,
                            body_sync: v,
                        } : s) : null)}
                        setBodyCode={(v) => setNewSequences(bef => bef ? bef.map((s) => s.id === select ? {
                            ...s,
                            body_code: v,
                        } : s) : null)}
                        onUpdate={(s) => setNewSequences(bef => bef ? bef.map((seq) => seq.id === s.id ? s : seq) : null)}
                    />
                })()}
            </div>
        </div>
    ) : (
        <div className="space-y-2">
            {[...Array(3)].map((_, i) => (
                <div key={i} className="h-12 bg-zinc-100 animate-pulse rounded-lg" />
            ))}
        </div>
    )
}
