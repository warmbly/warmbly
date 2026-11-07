// Mailboxes page strip for self-hosted instances. Unlinked: local warmup is not
// effective and self-hosting is unsupported, so it points at Warmbly Cloud.
// Linked on a free workspace: what it enrolled keeps warming, new mailboxes
// warm here unless the workspace pays. Linked and paid: one line of state.

import React from "react";
import { Link } from "@tanstack/react-router";
import { ArrowRightIcon, CloudIcon, ExternalLinkIcon, MessagesSquareIcon, ServerIcon, XIcon } from "lucide-react";
import useCloudPool from "@/hooks/useCloudPool";
import useEmails from "@/lib/api/hooks/app/emails/useEmails";
import { resolveMailboxWarmup, warmupUnknown } from "@/lib/mailboxWarmup";
import { CLOUD_APP_URL, COMMUNITY_URL, cloudAddsNeedSubscription } from "@/lib/cloudPlan";

// A new key, so instances that dismissed the old free offer see this notice once.
const DISMISS_KEY = "warmbly.selfHostNotice.dismissed";

// Counts cover the workspace's whole inventory, not the page's search or tag filter.
export default function CloudPoolBanner() {
    const pool = useCloudPool();
    const inventory = useEmails({ query: "", tag: "", enabled: pool.connected });
    const mailboxCount = inventory.emails.length;
    const enrolled = inventory.emails.filter((mailbox) => pool.rowFor(mailbox.id)?.enrolled).length;
    const states = inventory.emails.map((mailbox) => resolveMailboxWarmup(mailbox, undefined, pool));
    const unknown = inventory.isPending || inventory.isLoadingRest || inventory.isError || inventory.isIncomplete || states.some(warmupUnknown);
    const active = states.filter((state) => state.source === "cloud" && state.activity === "active").length;
    const [dismissed, setDismissed] = React.useState(() => localStorage.getItem(DISMISS_KEY) === "1");

    if (!pool.manageable || pool.loading) return null;
    if (!pool.connected && pool.unavailable) return <p className="text-xs text-slate-500">Cloud warmup status unavailable.</p>;

    if (pool.connected) {
        const plan = pool.plan;
        const premium = plan?.tier === "paid";
        const closed = cloudAddsNeedSubscription(plan);
        const subscribeURL = plan?.upgrade_url || plan?.manage_url;
        if (!unknown && closed && mailboxCount > enrolled) {
            return (
                <Notice
                    icon={ServerIcon}
                    title="New mailboxes warm on this instance, which is not effective."
                    body={
                        <>
                            {enrolled > 0 ? `The ${enrolled} already in Warmbly Cloud keep warming there. ` : ""}
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
                            {unknown
                                ? "Cloud warmup status unavailable."
                                : `${enrolled} of ${mailboxCount} mailboxes connected to Warmbly Cloud. ${active} warming.`}
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
                    ones. To move, pick a plan on Warmbly Cloud, then bring this workspace over with an export from{" "}
                    <Link to="/app/settings/data" className="font-medium text-sky-700 hover:text-sky-900 underline underline-offset-2">
                        Settings &gt; Data
                    </Link>
                    . Warmbly does not provide support for self-hosting.
                </>
            }
            actions={
                <>
                    <a href={`${CLOUD_APP_URL}/auth/register?next=${encodeURIComponent(CLOUD_PLANS_PATH)}`} target="_blank" rel="noreferrer" className={PRIMARY}>
                        Upgrade on Warmbly Cloud
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

// Opens the plan chooser once the account exists; the sign-up page also signs existing users in.
const CLOUD_PLANS_PATH = "/app/settings/billing?pool=1";

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
