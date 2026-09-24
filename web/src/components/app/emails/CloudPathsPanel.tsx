// Hosted workspace, Accounts page: how to get mailboxes warming. A free
// workspace gets one card (connect a mailbox, 10 are free) with the Warmup
// plan as the way past the allowance; self-hosting is a footnote, not a
// column, because it is optional and reads as the point of the plan when it
// sits beside it. A subscribed workspace gets a one-line strip. Full panel
// when the list is empty, one strip once mailboxes exist.

import React from "react";
import { Link } from "react-router-dom";
import { motion } from "framer-motion";
import { CloudIcon, InboxIcon, PlusIcon } from "lucide-react";
import useFeatureAccess from "@/hooks/useFeatureAccess";
import useAuthConfig from "@/lib/api/hooks/auth/useAuthConfig";
import { usePoolLinkInstances } from "@/lib/api/hooks/app/cloudlink/useCloudLink";
import { getPlan } from "@/lib/plans";
import WarmupPlanDialog from "@/components/app/billing/WarmupPlanDialog";

const FREE_MAILBOXES = 10;

export default function CloudPathsPanel({
    mailboxCount,
    warmingCount,
    onAdd,
}: {
    mailboxCount: number;
    /** Mailboxes with warmup on and not paused; connected is not the same as warming. */
    warmingCount: number;
    onAdd: () => void;
}) {
    const authConfig = useAuthConfig();
    const access = useFeatureAccess();
    const hosted = authConfig.data?.self_hosted === false;
    const instances = usePoolLinkInstances(hosted);
    const [planOpen, setPlanOpen] = React.useState(false);
    // Both hops are optional: a list endpoint whose rows come back as a nil
    // slice serialises `data` as null, not [], so the inner one throws.
    const linked = instances.data?.data?.length ?? 0;
    const plan = instances.data?.plan;

    if (!hosted || access.loading) return null;
    // The pool plan decides this once it has loaded; the subscription only
    // stands in while it is in flight.
    const limit = plan ? plan.mailbox_limit : access.paid ? null : FREE_MAILBOXES;
    const used = plan?.enrolled ?? mailboxCount;
    const free = limit !== null;
    const allowance = limit ?? FREE_MAILBOXES;
    const canBuy = free && access.billing && access.isOwner;
    const warmup = getPlan("warmup");
    const onWarmupPlan = access.plan === "warmup";

    const dialog = <WarmupPlanDialog open={planOpen} onClose={() => setPlanOpen(false)} />;

    if (mailboxCount === 0 && linked === 0) {
        return (
            <div className="py-3">
                <motion.div initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} className="rounded-xl border border-slate-200 overflow-hidden">
                    <div className="px-6 py-7 text-center bg-gradient-to-b from-sky-50/70 to-white">
                        <span className="inline-flex items-center gap-1.5 h-6 px-2.5 rounded-full bg-white border border-sky-100 text-sky-700 text-[11px] font-medium">
                            <CloudIcon className="w-3 h-3" /> {free ? `${allowance} mailboxes free` : "Unlimited mailboxes"}
                        </span>
                        <h2 className="mt-3 text-[20px] font-semibold tracking-[-0.02em] text-slate-900">Start warming your mailboxes</h2>
                        <p className="mt-1.5 text-[13px] text-slate-500 max-w-md mx-auto leading-relaxed">
                            Gmail, Microsoft 365 or any SMTP/IMAP account. Warmup starts on its own in a pool of real mailboxes, replies and spam rescue included.
                        </p>
                        <div className="mt-5 flex flex-wrap items-center justify-center gap-2">
                            <button
                                type="button"
                                onClick={onAdd}
                                className="inline-flex items-center gap-1.5 h-8 px-3 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12.5px] font-medium transition-colors"
                            >
                                <PlusIcon className="w-3.5 h-3.5" /> Add account
                            </button>
                            {canBuy && (
                                <button type="button" onClick={() => setPlanOpen(true)} className="inline-flex items-center gap-1.5 h-8 px-3 rounded-md border border-slate-200 hover:border-slate-300 text-[12.5px] font-medium text-slate-800 transition-colors">
                                    <InboxIcon className="w-3.5 h-3.5 text-sky-600" /> Better deliverability, ${warmup.priceMonthly}/mo
                                </button>
                            )}
                        </div>
                    </div>
                    <div className="px-6 py-2.5 border-t border-slate-200/70 text-center text-[11.5px] text-slate-400">
                        Running Warmbly on your own server? It can warm its mailboxes here too:{" "}
                        <Link to="/connect" className="font-medium text-slate-600 hover:text-slate-900">
                            link your instance
                        </Link>
                        .
                    </div>
                </motion.div>
                {dialog}
            </div>
        );
    }

    const linkedLabel = linked > 0 ? ` ${linked} linked instance${linked === 1 ? "" : "s"}.` : "";

    return (
        <div>
            <div className="flex items-center gap-2.5 rounded-md border border-slate-200 bg-white px-3 py-2 text-[12.5px] text-slate-700">
                <CloudIcon className="w-4 h-4 shrink-0 text-sky-600" />
                <span className="min-w-0 flex-1 leading-snug">
                    <span className="font-medium">
                        {free ? `${used} of ${allowance} free mailboxes used. ` : ""}
                        {warmingCount === 0
                            ? "No mailbox is warming yet."
                            : `${warmingCount} of ${mailboxCount} mailbox${mailboxCount === 1 ? "" : "es"} warming in the ${onWarmupPlan ? "premium " : ""}pool.`}
                    </span>
                    {warmingCount === 0 && mailboxCount > 0 && (
                        <span className="text-slate-500"> Turn on warmup from a mailbox&apos;s Warmup tab, or select several and start it for all.</span>
                    )}
                    {(linkedLabel || (free && canBuy)) && (
                        <span className="text-slate-500">
                            {linkedLabel}
                            {free && canBuy ? ` Premium gives every mailbox better deliverability, $${warmup.priceMonthly}/mo.` : ""}
                        </span>
                    )}
                </span>
                {canBuy && (
                    <button
                        type="button"
                        onClick={() => setPlanOpen(true)}
                        className="shrink-0 h-6 px-2 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium transition-colors"
                    >
                        Upgrade
                    </button>
                )}
                {linked > 0 && (
                    <Link to="/app/settings/warmbly-cloud" className="shrink-0 text-[12px] font-medium text-sky-700 hover:text-sky-900 underline underline-offset-2">
                        Linked instances
                    </Link>
                )}
            </div>
            {dialog}
        </div>
    );
}
