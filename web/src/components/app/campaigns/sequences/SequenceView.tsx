// The campaign Step composer (the Original arm). It owns the step-level chrome
// (name, reset/save, the follow-up threading note) and delegates the actual
// email composing to the shared EmailContentEditor, so the original and every
// A/B variant are edited with the identical toolset (templates, AI, preview,
// content score).

import React from "react";
import { GitBranchIcon, Loader2Icon } from "lucide-react";
import toast from "react-hot-toast";
import type Sequence from "@/lib/api/models/app/campaigns/sequences/Sequence";
import EmailContentEditor from "./EmailContentEditor";
import { Label, TextInput } from "@/components/ui/field";
import useUpdateSequence from "@/lib/api/hooks/app/campaigns/sequences/useUpdateSequence";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

// Body fields the composer owns. body_sync/body_code are legacy editor-only
// flags (they don't affect sending), so the composer keeps HTML + plain in
// lockstep and leaves them alone.
type Draft = Pick<Sequence, "name" | "subject" | "body_plain" | "body_html">;

function toDraft(s: Sequence): Draft {
    return { name: s.name, subject: s.subject, body_plain: s.body_plain, body_html: s.body_html };
}

export default function SequenceView({
    campaignId,
    sequence,
    index,
    embedded = false,
}: {
    campaignId: string;
    sequence: Sequence;
    index: number;
    // When embedded inside the tabbed arms editor, drop the outer card chrome
    // and the "Step N" eyebrow (the tab already provides that context).
    embedded?: boolean;
}) {
    const updateSequence = useUpdateSequence(campaignId, sequence.id);

    const [load, setLoad] = React.useState(false);
    const [draft, setDraft] = React.useState<Draft>(() => toDraft(sequence));
    React.useEffect(() => {
        setDraft(toDraft(sequence));
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [sequence.id]);

    const baseline = toDraft(sequence);
    const savable = React.useMemo(() => JSON.stringify(baseline) !== JSON.stringify(draft), [baseline, draft]);
    const patch = (p: Partial<Draft>) => setDraft((d) => ({ ...d, ...p }));

    async function submit() {
        if (load || !savable) return;
        setLoad(true);
        try {
            const data: Partial<Sequence> = {
                ...(draft.name !== baseline.name && { name: draft.name }),
                ...(draft.subject !== baseline.subject && { subject: draft.subject }),
                ...(draft.body_plain !== baseline.body_plain && { body_plain: draft.body_plain }),
                ...(draft.body_html !== baseline.body_html && { body_html: draft.body_html }),
            };
            await toast.promise(updateSequence.mutateAsync(data), {
                loading: "Saving step…",
                success: "Step saved.",
                error: (err: AppError) => buildError(err),
            });
        } finally {
            setLoad(false);
        }
    }

    return (
        <div className={embedded ? "" : "rounded-md border border-slate-200 bg-white"}>
            <div
                className={`flex flex-col gap-3 px-3 py-2.5 sm:flex-row sm:items-center sm:justify-between ${
                    embedded ? "" : "border-b border-slate-200"
                }`}
            >
                {!embedded && (
                    <div className="min-w-0">
                        <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                            Step {index + 1}
                        </div>
                        <p className="mt-0.5 truncate text-[11px] text-slate-400">Compose the email this step sends.</p>
                    </div>
                )}
                <div className="flex flex-wrap shrink-0 items-center gap-2 sm:ml-auto">
                    <button
                        type="button"
                        onClick={() => setDraft(toDraft(sequence))}
                        disabled={!savable || load}
                        className="h-7 px-2.5 rounded-md border border-slate-200 bg-white text-[12px] font-medium text-slate-700 transition-colors hover:border-slate-300 hover:text-slate-900 disabled:opacity-40"
                    >
                        Reset
                    </button>
                    <button
                        type="button"
                        onClick={submit}
                        disabled={!savable || load}
                        className="h-7 px-3 rounded-md bg-sky-600 text-[12px] font-medium text-white transition-colors hover:bg-sky-700 inline-flex items-center gap-1.5 disabled:opacity-40"
                    >
                        {load && <Loader2Icon className="w-3 h-3 animate-spin" />}
                        Save changes
                    </button>
                </div>
            </div>

            <div className="space-y-4 p-3">
                <div>
                    <Label>Step name</Label>
                    <TextInput value={draft.name} onChange={(v) => patch({ name: v })} placeholder={`Step ${index + 1}`} />
                    <p className="mt-1.5 text-[10.5px] text-slate-400">Internal label only — recipients never see it.</p>
                </div>

                <EmailContentEditor
                    key={sequence.id}
                    subject={draft.subject}
                    onSubjectChange={(v) => patch({ subject: v })}
                    bodyHtml={draft.body_html}
                    onBodyChange={(html, plain) => patch({ body_html: html, body_plain: plain })}
                />

                {index > 0 && (
                    <div className="flex items-start gap-2 rounded-md border border-slate-200 bg-slate-50/60 px-3 py-2.5">
                        <GitBranchIcon className="mt-0.5 w-3.5 h-3.5 shrink-0 text-slate-400" />
                        <p className="text-[11px] leading-relaxed text-slate-500">
                            Follow-ups thread on the previous step&apos;s subject. Change this subject and the follow-up
                            starts a new thread instead of replying in the existing one.
                        </p>
                    </div>
                )}
            </div>
        </div>
    );
}
