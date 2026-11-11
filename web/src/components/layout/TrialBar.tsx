// App-wide countdown for a workspace in its free trial, so the end date is
// never a surprise. Hidden on every other workspace.

import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { SparklesIcon } from "lucide-react";
import useFeatureAccess, { useTrialDaysLeft } from "@/hooks/useFeatureAccess";
import { useUpgradeDialog } from "@/hooks/context/upgrade";

export default function TrialBar() {
    const access = useFeatureAccess();
    const upgrade = useUpgradeDialog();
    const qc = useQueryClient();
    const endsAt = access.trial ? access.trialEndsAt?.getTime() : undefined;
    const days = useTrialDaysLeft(access.trial ? access.trialEndsAt : null);

    // Re-read the server's answer when the trial ends, so an open tab locks with it.
    useEffect(() => {
        if (endsAt == null) return;
        const wait = Math.min(Math.max(endsAt - Date.now(), 0) + 1000, 2_000_000_000);
        const timer = setTimeout(() => void qc.invalidateQueries({ queryKey: ["subscription"] }), wait);
        return () => clearTimeout(timer);
    }, [endsAt, qc]);

    if (endsAt == null) return null;

    return (
        <div role="status" className="flex flex-wrap items-center gap-x-3 gap-y-1 px-4 py-2 border-b bg-sky-50 border-sky-200 text-sky-900">
            <SparklesIcon className="w-4 h-4 shrink-0 text-sky-600" />
            <span className="min-w-0 flex-1 text-[12.5px] leading-relaxed">
                <span className="font-medium">
                    {days} {days === 1 ? "day" : "days"} left in your free trial.
                </span>{" "}
                Pick a plan before it ends to keep sending and warming without a pause.
            </span>
            <button
                type="button"
                onClick={() =>
                    upgrade.open({
                        feature: "Your trial",
                        title: "Choose a plan to keep sending",
                        minPlan: "starter",
                        blurb: "Keep everything you set up in the trial, with every mailbox warming in the premium pool and more sends a day.",
                    })
                }
                className="h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
            >
                {access.isOwner ? "Choose a plan" : "See plans"}
            </button>
        </div>
    );
}
