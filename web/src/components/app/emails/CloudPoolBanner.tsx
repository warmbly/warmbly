// Mailboxes page strip for self-hosted instances: invites an unlinked
// instance to connect, and shows a linked one how many mailboxes are in the
// pool. Dismissal of the invite is remembered locally. Once linked the strip
// stays quiet: one line of state, and the Premium nudge only when the free
// allowance is actually used up.

import React from "react";
import { Link } from "@tanstack/react-router";
import { ArrowRightIcon, CloudIcon, ExternalLinkIcon, MailCheckIcon, TrendingUpIcon, XIcon } from "lucide-react";
import useCloudPool from "@/hooks/useCloudPool";

const DISMISS_KEY = "warmbly.cloud-pool-banner.dismissed";

export default function CloudPoolBanner({ onConnect, mailboxCount }: { onConnect: () => void; mailboxCount: number }) {
    const pool = useCloudPool();
    const [dismissed, setDismissed] = React.useState(() => localStorage.getItem(DISMISS_KEY) === "1");

    if (!pool.manageable || pool.loading) return null;

    if (pool.connected && !pool.workspaceConnected) {
        return <div className="px-3 py-2 text-[12.5px] text-slate-600">Existing Cloud mailboxes keep warming. <Link to="/app/settings/warmbly-cloud" className="font-medium text-sky-700 underline">Connect this workspace</Link> to add more.</div>;
    }
    if (pool.connected) {
        const plan = pool.plan;
        const limit = plan?.mailbox_limit ?? null;
        const premium = plan?.tier === "paid";
        // The allowance is per cloud workspace, so the cloud's count is the
        // one that decides, not this instance's own enrolled count.
        const used = plan?.enrolled ?? pool.enrolledCount;
        const atLimit = !premium && limit !== null && used >= limit && mailboxCount > pool.enrolledCount;
        if (atLimit && plan?.upgrade_url) {
            return (
                <div>
                    <div className="flex items-start sm:items-center gap-3 rounded-md border border-slate-200 bg-white px-3 py-2.5 text-[12.5px] text-slate-700">
                        <SoftIcon icon={TrendingUpIcon} />
                        <div className="min-w-0 flex-1 flex flex-col sm:flex-row sm:items-center gap-2 sm:gap-3">
                            <span className="min-w-0 flex-1 leading-snug">
                                <span className="font-medium text-slate-900">Give every mailbox better deliverability.</span>{" "}
                                <span className="text-slate-500">
                                    All {limit} free pool mailboxes are in use. Premium warms the rest in the premium pool, built for inbox placement, for ${plan.price_usd} a month.
                                </span>
                            </span>
                            <a
                                href={plan.upgrade_url}
                                target="_blank"
                                rel="noreferrer"
                                className="self-start sm:self-auto shrink-0 h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
                            >
                                Upgrade to Premium
                                <ExternalLinkIcon className="w-3 h-3" />
                            </a>
                        </div>
                    </div>
                </div>
            );
        }
        return (
            <div>
                <div className="flex items-center gap-2.5 rounded-md border border-slate-200 bg-white px-3 py-2 text-[12.5px] text-slate-700">
                    <CloudIcon className="w-4 h-4 shrink-0 text-sky-600" />
                    <span className="min-w-0 flex-1 leading-snug">
                        <span className="font-medium">
                            {pool.enrolledCount === 0
                                ? "No mailbox is warming in Warmbly Cloud yet."
                                : `${pool.enrolledCount} of ${mailboxCount} mailboxes warm in Warmbly Cloud.`}
                        </span>{" "}
                        <span className="text-slate-500">
                            {premium ? "Premium pool." : limit === null ? "Unlimited." : `${used} of ${limit} free.`}
                        </span>
                    </span>
                    <Link to="/app/settings/warmbly-cloud" className="shrink-0 text-[12px] font-medium text-sky-700 hover:text-sky-900 underline underline-offset-2">
                        Manage
                    </Link>
                </div>
            </div>
        );
    }

    if (dismissed) return null;

    return (
        <div>
            <div className="flex items-start sm:items-center gap-3 rounded-md border border-slate-200 bg-white px-3 py-2.5 text-[12.5px] text-slate-700">
                <SoftIcon icon={MailCheckIcon} />
                <div className="min-w-0 flex-1 flex flex-col sm:flex-row sm:items-center gap-2 sm:gap-3">
                    <span className="min-w-0 flex-1 leading-snug">
                        <span className="font-medium text-slate-900">Get these mailboxes into the inbox, not spam.</span>{" "}
                        <span className="text-slate-500">
                            Warm them in Warmbly Cloud's large, active pool to build sender reputation, with replies and spam rescue handled for you.
                            Free for 10 mailboxes, set up in a minute, and your data stays here.
                        </span>
                    </span>
                    <button
                        type="button"
                        onClick={onConnect}
                        className="self-start sm:self-auto shrink-0 h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
                    >
                        Connect for free
                        <ArrowRightIcon className="w-3 h-3" />
                    </button>
                </div>
                <button
                    type="button"
                    aria-label="Dismiss"
                    onClick={() => {
                        localStorage.setItem(DISMISS_KEY, "1");
                        setDismissed(true);
                    }}
                    className="shrink-0 size-6 rounded-md inline-flex items-center justify-center text-slate-400 hover:text-slate-700 hover:bg-slate-100"
                >
                    <XIcon className="w-3.5 h-3.5" />
                </button>
            </div>
        </div>
    );
}

// A soft round badge: tinted fill and a hairline ring, lighter than a solid tile.
function SoftIcon({ icon: Icon }: { icon: React.ComponentType<{ className?: string }> }) {
    return (
        <span className="size-8 rounded-full bg-sky-50 text-sky-600 ring-1 ring-sky-100 inline-flex items-center justify-center shrink-0" aria-hidden="true">
            <Icon className="w-4 h-4" />
        </span>
    );
}
