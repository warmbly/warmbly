import { useCallback, useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import toast from "react-hot-toast/headless";
import { useWriteReviewAIPass, refreshReview } from "@/lib/api/hooks/app/campaigns/useCampaignReview";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

// The server takes at most this many contact_ids per request.
const BATCH = 500;
export function chunked<T>(ids: T[]): T[][] {
    const out: T[][] = [];
    for (let i = 0; i < ids.length; i += BATCH) out.push(ids.slice(i, i + BATCH));
    return out;
}

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
    // Leaving Review ends the run after the pass in flight; what it wrote is kept.
    useEffect(
        () => () => {
            stopRef.current = true;
        },
        [],
    );

    const run = useCallback(
        async (body: { contact_ids?: string[]; waiting_only?: boolean; step_id?: string }) => {
            stopRef.current = false;
            const total: BulkAIProgress = { written: 0, failed: 0, remaining: 0 };
            setProgress({ ...total });
            const parts: (string[] | undefined)[] = body.contact_ids ? chunked(body.contact_ids) : [undefined];
            try {
                run: for (const [i, ids] of parts.entries()) {
                    let after: string | undefined;
                    for (;;) {
                        const res = await pass.mutateAsync({ ...body, contact_ids: ids, after });
                        after = res.next || after;
                        total.written += res.written;
                        total.failed += res.failed;
                        // Later batches are not counted yet, so they are added to what this one has left.
                        total.remaining = res.remaining + parts.slice(i + 1).reduce((n, p) => n + (p?.length ?? 0), 0);
                        setProgress({ ...total });
                        if (res.stopped === "insufficient_credits") {
                            toast.error("The balance ran out. What was written is saved; add money under Billing to write the rest.");
                            break run;
                        }
                        if (res.stopped === "usage_cap_exceeded") {
                            toast.error("The workspace reached its AI spend limit. What was written is saved.");
                            break run;
                        }
                        if (stopRef.current) break run;
                        if (res.remaining === 0 || res.written + res.failed === 0) break;
                    }
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
