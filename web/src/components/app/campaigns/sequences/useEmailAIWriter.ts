// useEmailAIWriter: "Write with AI" for a campaign step, as the same draft
// cycle the unibox reply runs (AIDraftBar), with the email streamed in as the
// model writes it.

import React from "react";
import toast from "react-hot-toast/headless";
import type { AIDraftController } from "@/components/app/ai/AIDraftBar";
import useWritingStream, { isAbort } from "@/components/app/ai/useWritingStream";
import { reportAIError } from "@/components/app/ai/reportAIError";
import { passageHTML } from "@/components/app/ai/richTextPassage";
import { type EmailAIContext, draftPrompt, parseDraft } from "./emailAIPrompt";

interface Draft {
    subject: string;
    html: string;
    plain: string;
}

export interface EmailAIWriter extends AIDraftController {
    // The one label for the status pill, following the real stage.
    busyLabels: string[];
}

type Stage = "thinking" | "writing" | "checking";

// useEmailAIWriter runs "Write with AI" as the same draft cycle the unibox
// reply uses: busy (sheen until the first words, then the email writing itself
// in), then review with Keep, Adjust, Retry and Discard, which puts back the
// email from before.
export function useEmailAIWriter({
    ctx,
    subjectLocked,
    subject,
    bodyPlain,
    bodyHtml,
    onApply,
}: {
    ctx: EmailAIContext;
    // The step carries the conversation's subject, so only the body is written.
    subjectLocked: boolean;
    subject: string;
    bodyPlain: string;
    bodyHtml: string;
    onApply: (subject: string | null, html: string) => void;
}): EmailAIWriter {
    const stream = useWritingStream();
    const [phase, setPhase] = React.useState<AIDraftController["phase"]>("idle");
    const [stage, setStage] = React.useState<Stage>("thinking");
    const [usage, setUsage] = React.useState<AIDraftController["usage"]>(null);
    const current = React.useRef<Draft>({ subject, html: bodyHtml, plain: bodyPlain });
    current.current = { subject, html: bodyHtml, plain: bodyPlain };
    const base = React.useRef<Draft>(current.current);
    const lastBrief = React.useRef("");
    const runId = React.useRef(0);
    const apply = React.useRef(onApply);
    apply.current = onApply;

    const restore = React.useCallback(() => {
        apply.current(subjectLocked ? null : base.current.subject, base.current.html);
    }, [subjectLocked]);

    // from is the email to write from: the author's, even on a rerun.
    const run = React.useCallback(
        async (brief: string, from: Draft) => {
            const id = ++runId.current;
            base.current = from;
            lastBrief.current = brief;
            setStage("thinking");
            setPhase("busy");
            let shown = false;
            const show = (text: string) => {
                // The subject line is complete before any of it is shown.
                if (!subjectLocked && !text.includes("\n")) return;
                const draft = parseDraft(text);
                if (!draft.body && !draft.subject) return;
                if (!shown) {
                    shown = true;
                    setStage("writing");
                }
                apply.current(subjectLocked ? null : (draft.subject ?? from.subject), passageHTML(draft.body));
            };
            try {
                const res = await stream.run(
                    { path: "/generation/write", body: { prompt: draftPrompt({ brief, ctx, wantSubject: !subjectLocked, currentBody: from.plain }) } },
                    {
                        onText: (partial) => runId.current === id && show(partial),
                        onChecking: () => runId.current === id && setStage("checking"),
                    },
                );
                if (runId.current !== id) return;
                setUsage({ chargedUsd: res.charged_usd ?? 0, tokens: res.tokens_used ?? 0 });
                const draft = parseDraft(res.text);
                if (!draft.body) {
                    restore();
                    toast.error("The AI returned an empty email. Try again.");
                    setPhase("idle");
                    return;
                }
                apply.current(subjectLocked ? null : (draft.subject ?? from.subject), passageHTML(draft.body));
                setPhase("review");
            } catch (e) {
                if (runId.current !== id) return;
                // Stopped after words arrived: keep them, for review.
                if (isAbort(e) && shown) {
                    setPhase("review");
                    return;
                }
                if (!isAbort(e)) reportAIError(e);
                restore();
                setPhase("idle");
            }
        },
        [ctx, restore, stream, subjectLocked],
    );

    const n = ctx.earlier.length;
    const label =
        stage === "checking"
            ? "Checking it against spam filters…"
            : stage === "writing"
              ? n > 0
                  ? `Writing follow-up ${n}…`
                  : "Writing the email…"
              : n > 0
                ? `Reading the ${n === 1 ? "earlier email" : `${n} earlier emails`}…`
                : "Reading your brief…";
    return {
        phase,
        usage,
        question: null,
        grounding: null,
        busyLabels: [label],
        streaming: phase === "busy" && stage !== "thinking",
        stop: stream.stop,
        start: (instruction) => {
            if (phase !== "busy") void run(instruction ?? "", current.current);
        },
        keep: () => setPhase("idle"),
        discard: () => {
            restore();
            setPhase("idle");
        },
        regenerate: () => {
            restore();
            void run(lastBrief.current, base.current);
        },
        adjust: (instruction) => {
            restore();
            void run(instruction, base.current);
        },
        answer: () => undefined,
        cancel: () => {
            runId.current++;
            stream.stop();
            restore();
            setPhase("idle");
        },
    };
}
