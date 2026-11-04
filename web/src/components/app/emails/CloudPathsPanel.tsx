// Hosted workspace, Accounts page: how to get mailboxes warming. A free
// workspace gets one card (connect a mailbox, 10 are free) with a paid plan
// as the way past the allowance; self-hosting is a footnote, not a column.
// A subscribed workspace gets a one-line strip. Full panel when the list is
// empty, one strip once mailboxes exist.

import { Link } from "@tanstack/react-router";
import { motion } from "framer-motion";
import { CloudIcon, InboxIcon, PlusIcon } from "lucide-react";
import useFeatureAccess from "@/hooks/useFeatureAccess";
import useAuthConfig from "@/lib/api/hooks/auth/useAuthConfig";
import { usePoolLinkInstances } from "@/lib/api/hooks/app/cloudlink/useCloudLink";
import { isWarmupOnlyPlan } from "@/lib/plans";

const FREE_MAILBOXES = 10;

export default function CloudPathsPanel({ mailboxCount, onAdd }: { mailboxCount: number; onAdd: () => void }) {
    const authConfig = useAuthConfig();
    const access = useFeatureAccess();
    const hosted = authConfig.data?.self_hosted === false;
    const instances = usePoolLinkInstances(hosted);
    // Both hops are optional: a list endpoint whose rows come back as a nil
    // slice serialises `data` as null, not [], so the inner one throws.
    const linked = instances.data?.data?.length ?? 0;
    const plan = instances.data?.plan;

    if (!hosted || access.loading) return null;
    // The pool plan decides this once it has loaded; the subscription only
    // stands in while it is in flight.
    const limit = plan ? plan.mailbox_limit : access.paid ? null : FREE_MAILBOXES;
    const used = plan?.enrolled ?? mailboxCount;
    // The server counts the whole workspace; the page's list is filtered and paged, so it only stands in for the total.
    const total = plan?.enrolled ?? mailboxCount;
    // Only the server knows how many warm across the workspace; without it the banner says nothing about warming.
    const warming = plan?.warming;
    const free = limit !== null;
    const allowance = limit ?? FREE_MAILBOXES;
    const canBuy = free && access.billing && access.isOwner;
    const onWarmupPlan = isWarmupOnlyPlan(access.plan);

    if (total === 0 && linked === 0) {
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
                                <Link to="/app/settings/billing/{-$tab}" params={{ tab: undefined }} className="inline-flex items-center gap-1.5 h-8 px-3 rounded-md border border-slate-200 hover:border-slate-300 text-[12.5px] font-medium text-slate-800 transition-colors">
                                    <InboxIcon className="w-3.5 h-3.5 text-sky-600" /> Premium pool on a paid plan
                                </Link>
                            )}
                        </div>
                    </div>
                </motion.div>
            </div>
        );
    }

    const linkedLabel = linked > 0 ? ` ${linked} linked instance${linked === 1 ? "" : "s"}.` : "";

    return (
        <div>
            <div className="flex flex-wrap items-center gap-x-2.5 gap-y-1.5 rounded-md border border-slate-200 bg-white px-3 py-2 text-[12.5px] text-slate-700">
                <CloudIcon className="w-4 h-4 shrink-0 text-sky-600" />
                <span className="min-w-0 flex-1 basis-56 leading-snug">
                    <span className="font-medium">
                        {free ? `${used} of ${allowance} free mailboxes used. ` : ""}
                        {warming === undefined
                            ? free
                                ? ""
                                : `${total} mailbox${total === 1 ? "" : "es"} connected.`
                            : warming === 0
                              ? "No mailbox is warming yet."
                              : `${warming} of ${total} mailbox${total === 1 ? "" : "es"} warming in the ${onWarmupPlan ? "premium " : ""}pool.`}
                    </span>
                    {warming === 0 && total > 0 && (
                        <span className="text-slate-500"> Turn on warmup from a mailbox&apos;s Warmup tab, or select several and start it for all.</span>
                    )}
                    {(linkedLabel || (free && canBuy)) && (
                        <span className="text-slate-500">
                            {linkedLabel}
                            {free && canBuy ? " Paid plans warm every mailbox in the premium pool." : ""}
                        </span>
                    )}
                </span>
                {canBuy && (
                    <Link
                        to="/app/settings/billing/{-$tab}"
                        params={{ tab: undefined }}
                        className="shrink-0 h-6 px-2 inline-flex items-center rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium transition-colors"
                    >
                        Upgrade
                    </Link>
                )}
                {linked > 0 && (
                    <Link to="/app/settings/warmbly-cloud" className="shrink-0 text-[12px] font-medium text-sky-700 hover:text-sky-900 underline underline-offset-2">
                        Linked instances
                    </Link>
                )}
            </div>
        </div>
    );
}
