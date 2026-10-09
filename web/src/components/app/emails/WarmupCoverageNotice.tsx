import { RiFireLine } from "@remixicon/react";

// Warmup builds sender reputation by having mailboxes exchange mail with each
// other. Self-hosted, the pool is just this instance's own mailboxes, so it has
// to be sizeable to look real: a single mailbox cannot warm at all (the backend
// never pairs a mailbox with itself), and a small pool pairs the same accounts
// over and over, which reads as artificial and can't safely ramp toward a real
// 40 to 50 a day.
//
// HEALTHY_MIN is the industry "minimum effective pool" line: warmup pools want
// roughly 20 to 50+ real mailboxes (a pool of ~10 exchanging mail looks fake).
// On a large shared hosted pool one mailbox already has hundreds of partners, so
// if that ever ships this notice (and the cloud pitch) should be gated to
// self-host.
const HEALTHY_MIN = 20;

export default function WarmupCoverageNotice({
    warmupCount,
    totalCount,
    canWarmup,
    onAdd,
    onConnectCloud,
    cloudConnected = false,
}: {
    warmupCount: number;
    totalCount: number;
    canWarmup: boolean;
    onAdd: () => void;
    /** Self-hosted and not linked: opens the connect dialog instead of the marketing site. */
    onConnectCloud?: () => void;
    /** Linked already: the pool advice is moot, so the notice stays quiet. */
    cloudConnected?: boolean;
}) {
    // Only relevant once at least one mailbox is actually warming, the org can
    // use warmup, and the pool is below a believable size.
    if (!canWarmup || cloudConnected || warmupCount < 1 || warmupCount >= HEALTHY_MIN) return null;

    const critical = warmupCount < 2;
    const hasIdle = totalCount > warmupCount;

    const title = critical
        ? "Warmup can't run with one mailbox"
        : "Too few mailboxes for healthy warmup";

    const body = critical
        ? "It works by having your mailboxes send to each other, so one mailbox has nobody to pair with."
        : `Only ${warmupCount} are warming, so the same accounts pair over and over, which reads as fake and ramps slowly.`;

    return (
        <div>
            <div className="flex items-start gap-2.5 rounded-md border border-amber-200/70 bg-amber-50/70 px-3 py-2.5 text-amber-800">
                <RiFireLine className="w-4 h-4 mt-px shrink-0 text-amber-500" />
                <div className="min-w-0 text-[12.5px] leading-snug">
                    <span className="font-medium">{title}.</span>{" "}
                    <span className="text-amber-800/90">
                        {body}
                        <span className="hidden sm:inline"> A believable pool is around 20 to 50 mailboxes, ramped over a few weeks toward a safe 40 to 50 a day.</span>
                    </span>{" "}
                    <button
                        type="button"
                        onClick={onAdd}
                        className="font-medium underline underline-offset-2 hover:text-amber-950"
                    >
                        Add mailboxes
                    </button>
                    <span className="text-amber-800/90">
                        {hasIdle ? " (or turn warmup on for the ones you already have)" : ""}, or let{" "}
                    </span>
                    {onConnectCloud ? (
                        <button type="button" onClick={onConnectCloud} className="font-medium underline underline-offset-2 hover:text-amber-950">
                            Warmbly Cloud
                        </button>
                    ) : (
                        <a
                            href="https://docs.warmbly.com/guides/warmbly-cloud/"
                            target="_blank"
                            rel="noopener noreferrer"
                            className="font-medium underline underline-offset-2 hover:text-amber-950"
                        >
                            Warmbly Cloud
                        </a>
                    )}
                    <span className="text-amber-800/90"> warm them against its large shared pool.</span>
                </div>
            </div>
        </div>
    );
}
