import { Link } from "@tanstack/react-router";
import { ArrowRightIcon, SparklesIcon, UserPenIcon } from "lucide-react";
import type Contact from "@/lib/api/models/app/contacts/Contact";
import type { TemplatePreview } from "@/lib/api/client/app/campaigns/previewTemplate";
import { cn } from "@/lib/utils";
import type { PreviewAIState } from "./usePreviewLeadAI";

function Note({ tone, icon: Icon, children }: { tone: "sky" | "amber" | "slate" | "rose"; icon: React.ElementType; children: React.ReactNode }) {
    const tones = {
        sky: "bg-sky-50/70 text-sky-800",
        amber: "bg-amber-50 text-amber-800",
        slate: "bg-slate-50 text-slate-600",
        rose: "bg-rose-50 text-rose-800",
    };
    return (
        <div className={cn("flex items-start gap-1.5 border-b border-slate-200/70 px-3 py-1.5 text-[11.5px] leading-relaxed", tones[tone])}>
            <Icon className="mt-[3px] w-3 h-3 shrink-0" />
            <div className="min-w-0 flex-1">{children}</div>
        </div>
    );
}

// What the step preview cannot show on its own: a lead's own copy, and AI copy
// that is written per lead.
export default function PreviewLeadNotes({
    campaignId,
    stepId,
    contact,
    preview,
    dirty,
    ai,
}: {
    campaignId: string;
    stepId: string;
    contact: Contact | null;
    preview: TemplatePreview | null;
    dirty?: boolean;
    ai: { state?: PreviewAIState; pending: { id: string; name: string }[]; canWrite: boolean; write: () => void };
}) {
    if (!preview) return null;
    const first = contact?.first_name?.trim() || "This lead";
    const blocks = preview.ai_blocks ?? [];
    const review = contact && (
        <Link
            to="/app/campaigns/$id/steps"
            params={{ id: campaignId }}
            search={{ review: "1", step: stepId, lead: contact.id }}
            className="ml-1 inline-flex items-center gap-0.5 font-medium underline-offset-2 hover:underline"
        >
            Open on the Review tab <ArrowRightIcon className="w-3 h-3" />
        </Link>
    );

    if (contact && preview.personalized) {
        return (
            <Note tone="amber" icon={UserPenIcon}>
                {first} has their own copy of this email, so they get that instead of this one.{review}
            </Note>
        );
    }
    if (blocks.length === 0) return null;
    if (!contact) {
        return (
            <Note tone="slate" icon={SparklesIcon}>
                AI writes the marked part for each lead. Pick a lead above to see theirs.
            </Note>
        );
    }
    if (!preview.lead) {
        return (
            <Note tone="slate" icon={SparklesIcon}>
                AI copy is written only for this campaign&apos;s leads, so the marked part stays empty here.
            </Note>
        );
    }
    if (ai.pending.length === 0) {
        return (
            <Note tone="sky" icon={SparklesIcon}>
                The highlighted part is what AI wrote for {first}.{review}
            </Note>
        );
    }
    if (ai.state === "writing") {
        return (
            <Note tone="sky" icon={SparklesIcon}>
                <span className="text-shimmer">AI is writing the marked part for {first}…</span>
            </Note>
        );
    }
    // A write reads the saved step, so unsaved edits take the retry away.
    const retry = !dirty && ai.canWrite && (
        <>
            {" "}
            <button type="button" onClick={ai.write} className="font-medium underline underline-offset-2">
                Try again
            </button>
        </>
    );
    if (ai.state === "balance") {
        return (
            <Note tone="amber" icon={SparklesIcon}>
                The AI balance is used up, so the marked part is written when the email sends, once there is balance.{retry}
            </Note>
        );
    }
    if (ai.state === "cap") {
        return (
            <Note tone="amber" icon={SparklesIcon}>
                The workspace reached its AI spend limit, so the marked part is written when the email sends, once the limit allows.{retry}
            </Note>
        );
    }
    if (ai.state === "unavailable") {
        return (
            <Note tone="slate" icon={SparklesIcon}>
                This instance has no AI provider set up, so the marked part cannot be written.
            </Note>
        );
    }
    if (ai.state === "error" && !dirty) {
        return (
            <Note tone="rose" icon={SparklesIcon}>
                The marked part could not be written for {first} just now.{retry}
            </Note>
        );
    }
    return (
        <Note tone="slate" icon={SparklesIcon}>
            {dirty
                ? `Save the step to see what AI writes for ${first}.`
                : ai.canWrite
                  ? `AI writes the marked part for ${first} before this email sends.`
                  : `AI writes the marked part for ${first} when the email sends.`}
        </Note>
    );
}
