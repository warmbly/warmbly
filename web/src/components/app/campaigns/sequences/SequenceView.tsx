import React from "react";
import { GitBranchIcon, Loader2Icon } from "lucide-react";
import toast from "react-hot-toast";
import type Sequence from "@/lib/api/models/app/campaigns/sequences/Sequence";
import EmailEditor from "../../EmailEditor";
import ContentScore from "../ContentScore";
import { Label, TextInput } from "@/components/ui/field";
import useUpdateSequence from "@/lib/api/hooks/app/campaigns/sequences/useUpdateSequence";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

// Editable subset of a step. wait_after is edited from the stepper rail, so the
// editor pane only owns the content fields — but the save still diffs the same
// body_sync/body_html/body_plain/body_code semantics the original used.
type Draft = Pick<
    Sequence,
    "name" | "subject" | "body_plain" | "body_html" | "body_sync" | "body_code"
>;

function toDraft(s: Sequence): Draft {
    return {
        name: s.name,
        subject: s.subject,
        body_plain: s.body_plain,
        body_html: s.body_html,
        body_sync: s.body_sync,
        body_code: s.body_code,
    };
}

export default function SequenceView({
    campaignId,
    sequence,
    index,
}: {
    campaignId: string;
    sequence: Sequence;
    index: number;
}) {
    const updateSequence = useUpdateSequence(campaignId, sequence.id);
    const [load, setLoad] = React.useState(false);

    // Draft is seeded from the canonical sequence and reset whenever a different
    // step is selected (or the underlying record changes). The committed
    // baseline lives in `sequence`; `draft` is the local working copy.
    const [draft, setDraft] = React.useState<Draft>(() => toDraft(sequence));
    React.useEffect(() => {
        setDraft(toDraft(sequence));
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [sequence.id]);

    const baseline = toDraft(sequence);
    const savable = React.useMemo(
        () => JSON.stringify(baseline) !== JSON.stringify(draft),
        [baseline, draft],
    );

    const patch = (p: Partial<Draft>) => setDraft((d) => ({ ...d, ...p }));

    async function submit() {
        if (load || !savable) return;
        setLoad(true);
        try {
            // Diff against the committed baseline so we only PATCH changed
            // fields — identical semantics to the previous implementation.
            const data: Partial<Sequence> = {
                ...(draft.name !== baseline.name && { name: draft.name }),
                ...(draft.subject !== baseline.subject && { subject: draft.subject }),
                ...(draft.body_plain !== baseline.body_plain && { body_plain: draft.body_plain }),
                ...(draft.body_html !== baseline.body_html && { body_html: draft.body_html }),
                ...(draft.body_sync !== baseline.body_sync && { body_sync: draft.body_sync }),
                ...(draft.body_code !== baseline.body_code && { body_code: draft.body_code }),
            };
            await toast.promise(updateSequence.mutateAsync(data), {
                loading: "Saving step…",
                success: "Step saved.",
                error: (err: AppError) => buildError(err),
            });
            // The mutation hook writes the fresh record into the query cache; the
            // `sequence` prop will update and the effect above re-seeds the draft.
        } finally {
            setLoad(false);
        }
    }

    return (
        <div className="rounded-md border border-slate-200 bg-white">
            <div className="flex items-center justify-between gap-3 border-b border-slate-200 px-3 py-2.5">
                <div className="min-w-0">
                    <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                        Step {index + 1}
                    </div>
                    <p className="mt-0.5 truncate text-[11px] text-slate-400">
                        Edit the email this step sends.
                    </p>
                </div>
                <div className="flex shrink-0 items-center gap-2">
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
                    <TextInput
                        value={draft.name}
                        onChange={(v) => patch({ name: v })}
                        placeholder={`Step ${index + 1}`}
                    />
                    <p className="mt-1.5 text-[10.5px] text-slate-400">
                        Internal label only — recipients never see it.
                    </p>
                </div>

                <div>
                    <Label>Subject</Label>
                    <TextInput
                        value={draft.subject}
                        onChange={(v) => patch({ subject: v })}
                        placeholder="Subject line"
                    />
                </div>

                <div>
                    <Label>Body</Label>
                    <EmailEditor
                        key={sequence.id}
                        id={`sequence-edit-${sequence.id}`}
                        htmlText={draft.body_html}
                        plainText={draft.body_plain}
                        sync={draft.body_sync}
                        code={draft.body_code}
                        setHtmlText={(v) => patch({ body_html: v })}
                        setPlainText={(v) => patch({ body_plain: v })}
                        setSync={(v) => patch({ body_sync: v })}
                        setCode={(v) => patch({ body_code: v })}
                    />
                </div>

                {index > 0 && (
                    <div className="flex items-start gap-2 rounded-md border border-slate-200 bg-slate-50/60 px-3 py-2.5">
                        <GitBranchIcon className="mt-0.5 w-3.5 h-3.5 shrink-0 text-slate-400" />
                        <p className="text-[11px] leading-relaxed text-slate-500">
                            Follow-ups thread on the previous step&apos;s subject. Change this
                            subject and the follow-up starts a new thread instead of replying in
                            the existing one.
                        </p>
                    </div>
                )}

                <ContentScore
                    subject={draft.subject}
                    bodyHtml={draft.body_html}
                    bodyPlain={draft.body_plain}
                />
            </div>
        </div>
    );
}
