// The campaign Step composer (the Original arm). It owns the step-level chrome
// (name, reset/save, the follow-up threading note) and delegates the actual
// email composing to the shared EmailContentEditor, so the original and every
// A/B variant are edited with the identical toolset (templates, AI, preview,
// content score).

import React from "react";
import { Loader2Icon, ReplyIcon } from "lucide-react";
import toast from "react-hot-toast";
import type Sequence from "@/lib/api/models/app/campaigns/sequences/Sequence";
import EmailContentEditor from "./EmailContentEditor";
import StepAttachments from "./StepAttachments";
import { Label, TextInput } from "@/components/ui/field";
import { SettingRow, Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import useUpdateSequence from "@/lib/api/hooks/app/campaigns/sequences/useUpdateSequence";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

// Body fields the composer owns. body_code records that this step is authored
// as raw HTML, so reopening it shows the markup instead of handing it to the
// editor schema, which keeps only what it can represent. body_sync stays a
// legacy editor-only flag: the composer keeps HTML + plain in lockstep itself.
type Draft = Pick<Sequence, "name" | "subject" | "body_plain" | "body_html" | "body_code" | "thread_reply">;

function toDraft(s: Sequence): Draft {
    return {
        name: s.name,
        subject: s.subject,
        body_plain: s.body_plain,
        body_html: s.body_html,
        body_code: s.body_code,
        thread_reply: s.thread_reply,
    };
}

export default function SequenceView({
    campaignId,
    sequence,
    index,
    conversationSubject = null,
    embedded = false,
    headerExtra,
}: {
    campaignId: string;
    sequence: Sequence;
    index: number;
    // The subject of the conversation this step would reply on, taken from the
    // step that opened it. Shown in place of the subject field while the step
    // replies in the thread, because that is what the recipient reads. `null`
    // means there is no earlier email to reply to, so the switch is hidden.
    conversationSubject?: string | null;
    // When embedded inside the tabbed arms editor, drop the outer card chrome
    // and the "Step N" eyebrow (the tab already provides that context).
    embedded?: boolean;
    // Optional control rendered to the left of Reset/Save in the header (used to
    // surface the "A/B test this step" entry point when no variants exist yet).
    headerExtra?: React.ReactNode;
}) {
    const updateSequence = useUpdateSequence(campaignId, sequence.id);

    const [load, setLoad] = React.useState(false);
    const [draft, setDraft] = React.useState<Draft>(() => toDraft(sequence));
    React.useEffect(() => {
        setDraft(toDraft(sequence));
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [sequence.id]);

    const baseline = toDraft(sequence);
    // A step with nothing before it opens the conversation, so it always
    // writes its own subject however the switch is set.
    const canThread = conversationSubject !== null;
    const threads = canThread && draft.thread_reply;
    // A conversation whose opener has no subject yet has nothing to lend, so
    // the step keeps writing its own. Mirrors models.StepSubject on the server.
    const inheritsSubject = threads && !!conversationSubject;
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
                ...(draft.body_code !== baseline.body_code && { body_code: draft.body_code }),
                ...(draft.thread_reply !== baseline.thread_reply && { thread_reply: draft.thread_reply }),
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
                    {headerExtra}
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

                {canThread && (
                    <div className="rounded-md border border-slate-200 bg-slate-50/60 px-3 py-2.5">
                        <SettingRow
                            title="Reply in thread"
                            description={
                                <span className="inline-flex items-start gap-1.5">
                                    <ReplyIcon className="mt-0.5 w-3 h-3 shrink-0 text-slate-400" />
                                    <span>
                                        Send this step as a reply on the conversation the contact is already in, so it
                                        lands under the email they have instead of arriving as a second cold email.
                                        Turn it off to start a fresh conversation with its own subject.
                                    </span>
                                </span>
                            }
                            control={
                                <Toggle
                                    value={draft.thread_reply}
                                    onChange={(v) => patch({ thread_reply: v })}
                                />
                            }
                        />
                    </div>
                )}

                <EmailContentEditor
                    key={sequence.id}
                    subject={draft.subject}
                    onSubjectChange={(v) => patch({ subject: v })}
                    subjectLocked={
                        inheritsSubject
                            ? {
                                  subject: conversationSubject ?? "",
                                  note: "A reply carries the conversation's subject. Turn off Reply in thread to write your own.",
                              }
                            : undefined
                    }
                    bodyHtml={draft.body_html}
                    onBodyChange={(html, plain) => patch({ body_html: html, body_plain: plain })}
                    bodyCode={draft.body_code}
                    onBodyCodeChange={(code) => patch({ body_code: code })}
                    campaignId={campaignId}
                    stepId={sequence.id}
                    canSendTest
                    dirty={savable}
                />

                {sequence.kind === "email" && (
                    <StepAttachments campaignId={campaignId} sequenceId={sequence.id} />
                )}
            </div>
        </div>
    );
}
