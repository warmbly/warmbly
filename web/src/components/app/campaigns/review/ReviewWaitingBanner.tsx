// Shown on every campaign tab while leads wait for approval, so a review campaign
// that looks stalled says why and where to go: the first email's Review tab.

import { AnimatePresence, motion } from "framer-motion";
import { Link } from "@tanstack/react-router";
import { ClipboardCheckIcon } from "lucide-react";
import { useCampaignReview } from "@/lib/api/hooks/app/campaigns/useCampaignReview";

export default function ReviewWaitingBanner({ campaignId, enabled }: { campaignId: string; enabled: boolean }) {
    const summary = useCampaignReview(enabled ? campaignId : "");
    const waiting = enabled ? (summary.data?.counts.waiting ?? 0) : 0;
    return (
        <AnimatePresence initial={false}>
            {waiting > 0 && (
                <motion.div
                    key="review-waiting"
                    initial={{ opacity: 0, y: -8, height: 0 }}
                    animate={{ opacity: 1, y: 0, height: "auto" }}
                    exit={{ opacity: 0, y: -8, height: 0 }}
                    transition={{ type: "spring", duration: 0.4, bounce: 0.2 }}
                    className="overflow-hidden"
                >
                    <div className="mx-3 sm:mx-5 mb-3 rounded-md border border-sky-200 bg-sky-50/70 px-3.5 py-2.5 flex items-center gap-3">
                        <span className="shrink-0 w-7 h-7 rounded-md bg-sky-100 text-sky-700 inline-flex items-center justify-center">
                            <ClipboardCheckIcon className="w-4 h-4" />
                        </span>
                        <p className="min-w-0 flex-1 text-[12.5px] text-sky-900">
                            {waiting.toLocaleString()} lead{waiting === 1 ? " is" : "s are"} waiting for review. Nothing goes to them until someone approves.
                        </p>
                        <Link
                            to="/app/campaigns/$id/steps"
                            params={{ id: campaignId }}
                            search={{ review: "1" }}
                            className="shrink-0 h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center transition-colors"
                        >
                            Review
                        </Link>
                    </div>
                </motion.div>
            )}
        </AnimatePresence>
    );
}
