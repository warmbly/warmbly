// Opt-in filter tag: a code on the last line of every warmup email this
// mailbox receives, for filtering warmup out in a mail client or another tool.

import toast from "react-hot-toast/headless";
import { Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import { CopyButton } from "@/components/app/emails/domains/parts";
import { useConfirm } from "@/hooks/context/confirm";
import { useSetWarmupFilterTag, useWarmupFilterTag } from "@/lib/api/hooks/app/emails/useWarmupFilterTag";
import buildError from "@/lib/helper/buildError";
import type { AppError } from "@/lib/api/client/normalizeError";

export default function WarmupFilterTagField({ mailboxId, provider }: { mailboxId: string; provider: string }) {
    const confirm = useConfirm();
    const { data, isLoading, isError } = useWarmupFilterTag(mailboxId);
    const save = useSetWarmupFilterTag(mailboxId);
    const tag = data?.tag ?? null;
    const run = async (next: string | null | undefined, done: string) => {
        try {
            await save.mutateAsync(next);
            toast.success(done);
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };
    const filterHint =
        provider === "gmail"
            ? `In Gmail, search for "${tag}", choose Create filter, then Skip the Inbox or Apply the label.`
            : provider === "outlook"
              ? `In Outlook, add a rule: the message body includes "${tag}", then move it to a folder.`
              : `In your mail client or tool, add a rule for mail whose body contains "${tag}".`;

    return (
        <div>
            <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                    <div className="text-[12px] font-medium text-slate-700">Filter tag</div>
                    <p className="text-[10.5px] text-slate-400 mt-0.5 leading-relaxed">
                        Off by default. When on, every warmup email this mailbox receives ends with the code below, so another
                        inbox tool or a mail rule can filter it out. Warmbly still files warmup as set above. The code is the same
                        on every warmup email to this mailbox, so turn it on only if you need it.
                    </p>
                </div>
                <Toggle
                    value={!!tag}
                    disabled={isLoading || isError || save.isPending}
                    onChange={(on) => void run(on ? undefined : null, on ? "Filter tag on" : "Filter tag off")}
                    ariaLabel="Add a filter tag to warmup mail this mailbox receives"
                />
            </div>
            {isError && <p className="mt-1 text-[11.5px] text-rose-600">The filter tag could not be loaded.</p>}
            {tag && (
                <div className="mt-2 space-y-1.5">
                    <div className="flex items-center gap-1.5">
                        <code className="min-w-0 flex-1 truncate rounded-md border border-slate-200 bg-slate-50 px-2 h-7 inline-flex items-center font-mono text-[12px] text-slate-800">
                            {tag}
                        </code>
                        <CopyButton value={tag} label="filter tag" />
                        <button
                            type="button"
                            disabled={save.isPending}
                            onClick={() =>
                                confirm.show(
                                    "Issue a new code? Filters that use the current code stop matching new warmup mail.",
                                    () => run(undefined, "New filter tag issued"),
                                )
                            }
                            className="h-7 px-2.5 shrink-0 rounded-md border border-slate-200 text-[12px] font-medium text-slate-600 hover:border-slate-300 hover:text-slate-800 disabled:opacity-50"
                        >
                            New code
                        </button>
                    </div>
                    <p className="text-[10.5px] text-slate-400 leading-relaxed">{filterHint}</p>
                </div>
            )}
        </div>
    );
}
