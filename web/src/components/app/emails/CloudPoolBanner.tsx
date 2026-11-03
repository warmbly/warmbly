// Mailboxes page strip for self-hosted instances. Unlinked: local warmup is not
// effective and self-hosting is unsupported, so it points at Warmbly Cloud.
// Linked on a free workspace: what it enrolled keeps warming, new mailboxes
// warm here unless the workspace pays. Linked and paid: one line of state.

import React from "react";
import { Link } from "@tanstack/react-router";
import { ArrowRightIcon, CloudIcon, ExternalLinkIcon, MessagesSquareIcon, ServerIcon, XIcon } from "lucide-react";
import useCloudPool from "@/hooks/useCloudPool";
import { COMMUNITY_URL, cloudAddsNeedSubscription } from "@/lib/cloudPlan";

// A new key, so instances that dismissed the old free offer see this notice once.
const DISMISS_KEY = "warmbly.selfHostNotice.dismissed";

export default function CloudPoolBanner({ mailboxCount }: { mailboxCount: number }) {
    const pool = useCloudPool();
    const [dismissed, setDismissed] = React.useState(() => localStorage.getItem(DISMISS_KEY) === "1");

    if (!pool.manageable || pool.loading) return null;

    if (pool.connected) {
        const plan = pool.plan;
        const premium = plan?.tier === "paid";
        const closed = cloudAddsNeedSubscription(plan);
        const subscribeURL = plan?.upgrade_url || plan?.manage_url;
        if (closed && mailboxCount > pool.enrolledCount) {
            return (
                <Notice
                    icon={ServerIcon}
                    title="New mailboxes warm on this instance, which is not effective."
                    body={
                        <>
                            {pool.enrolledCount > 0 ? `The ${pool.enrolledCount} already in Warmbly Cloud keep warming there. ` : ""}
                            Adding more to the Cloud pool needs a paid Warmbly Cloud plan. Self-hosted instances get no support from Warmbly.
                        </>
                    }
                    actions={
                        <>
                            {subscribeURL && (
                                <a href={subscribeURL} target="_blank" rel="noreferrer" className={PRIMARY}>
                                    See plans
                                    <ExternalLinkIcon className="w-3 h-3" />
                                </a>
                            )}
                            <CommunityLink />
                        </>
                    }
                />
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
                        <span className="text-slate-500">{premium ? "Premium pool." : closed ? "Free, no new mailboxes." : "Free pool."}</span>
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
        <Notice
            icon={ServerIcon}
            title="Warmup on this instance is not effective."
            body={
                <>
                    It only pairs your own mailboxes, which providers learn to recognise. Warmbly Cloud warms mailboxes in a pool of thousands of real
                    ones. Self-hosting is for experienced operators, and Warmbly does not provide support for it.
                </>
            }
            actions={
                <>
                    <a href={pool.cloudURL} target="_blank" rel="noreferrer" className={PRIMARY}>
                        Use Warmbly Cloud
                        <ArrowRightIcon className="w-3 h-3" />
                    </a>
                    <CommunityLink />
                </>
            }
            onDismiss={() => {
                localStorage.setItem(DISMISS_KEY, "1");
                setDismissed(true);
            }}
        />
    );
}

const PRIMARY = "shrink-0 h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors";

function Notice({ icon, title, body, actions, onDismiss }: { icon: React.ComponentType<{ className?: string }>; title: string; body: React.ReactNode; actions: React.ReactNode; onDismiss?: () => void }) {
    return (
        <div>
            <div className="flex items-start sm:items-center gap-3 rounded-md border border-slate-200 bg-white px-3 py-2.5 text-[12.5px] text-slate-700">
                <SoftIcon icon={icon} />
                <div className="min-w-0 flex-1 flex flex-col sm:flex-row sm:items-center gap-2 sm:gap-3">
                    <span className="min-w-0 flex-1 leading-snug">
                        <span className="font-medium text-slate-900">{title}</span> <span className="text-slate-500">{body}</span>
                    </span>
                    <div className="self-start sm:self-auto shrink-0 flex items-center gap-2">{actions}</div>
                </div>
                {onDismiss && (
                    <button
                        type="button"
                        aria-label="Dismiss"
                        onClick={onDismiss}
                        className="shrink-0 size-6 rounded-md inline-flex items-center justify-center text-slate-400 hover:text-slate-700 hover:bg-slate-100"
                    >
                        <XIcon className="w-3.5 h-3.5" />
                    </button>
                )}
            </div>
        </div>
    );
}

function CommunityLink() {
    return (
        <a
            href={COMMUNITY_URL}
            target="_blank"
            rel="noreferrer"
            className="shrink-0 h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-slate-700 text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
        >
            <MessagesSquareIcon className="w-3 h-3 text-slate-500" />
            Community on Discord
        </a>
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
