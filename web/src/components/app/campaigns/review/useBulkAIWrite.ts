import { useCallback, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import toast from "react-hot-toast/headless";
import { useWriteReviewAIPass, refreshReview } from "@/lib/api/hooks/app/campaigns/useCampaignReview";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

export interface BulkAIProgress {
    written: number;
    failed: number;
    remaining: number;
}

// Writes AI copy ahead in server-sized passes until nothing is left, the
// balance runs out, or the member stops it. Each pass hands back a cursor past
// the leads it tried, so a lead that keeps failing cannot stall the run.
export function useBulkAIWrite(campaignId: string) {
    const qc = useQueryClient();
    const pass = useWriteReviewAIPass(campaignId);
    const [progress, setProgress] = useState<BulkAIProgress | null>(null);
    const stopRef = useRef(false);

    const run = useCallback(
        async (body: { contact_ids?: string[]; waiting_only?: boolean; step_id?: string }) => {
            stopRef.current = false;
            const total: BulkAIProgress = { written: 0, failed: 0, remaining: 0 };
            setProgress({ ...total });
            let after: string | undefined;
            try {
                for (;;) {
                    const res = await pass.mutateAsync({ ...body, after });
                    after = res.next || after;
                    total.written += res.written;
                    total.failed += res.failed;
                    total.remaining = res.remaining;
                    setProgress({ ...total });
                    if (res.stopped === "insufficient_credits") {
                        toast.error("The balance ran out. What was written is saved; add money under Billing to write the rest.");
                        break;
                    }
                    if (res.stopped === "usage_cap_exceeded") {
                        toast.error("The workspace reached its AI spend limit. What was written is saved.");
                        break;
                    }
                    if (stopRef.current || res.remaining === 0 || res.written + res.failed === 0) break;
                }
                if (total.failed > 0) {
                    toast.error(`${total.failed.toLocaleString()} could not be written. They are written when they send instead.`);
                } else if (total.written > 0) {
                    toast.success(`AI copy written for ${total.written.toLocaleString()} lead${total.written === 1 ? "" : "s"}`);
                }
            } catch (err) {
                toast.error(buildError(err as AppError));
            } finally {
                setProgress(null);
                refreshReview(qc, campaignId);
                void qc.invalidateQueries({ queryKey: ["subscription", "credits"] });
            }
        },
        [pass, qc, campaignId],
    );

    const stop = useCallback(() => {
        stopRef.current = true;
    }, []);

    return { run, stop, progress, running: progress !== null };
}
