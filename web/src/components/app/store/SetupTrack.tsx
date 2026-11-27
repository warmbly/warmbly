// The order being set up, live, in the card the summary became after Pay: a step counter and bar along the top,
// the current activity under the headline, and a rail that fills as each step lands. A finished order ends on a
// quiet celebration with the new mailboxes warming up.
import React from "react";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import { Link } from "@tanstack/react-router";
import { AlertTriangleIcon, ArrowRightIcon, ClockIcon, MinusIcon, SendIcon, XIcon } from "lucide-react";
import ProviderLogo from "@/components/app/emails/ProviderLogo";
import RollingNumber from "@/components/ui/RollingNumber";
import type { Order, StoreMailbox } from "@/lib/api/models/app/store/Store";
import { fmtMicros } from "@/lib/buy/money";
import { activityLine, deriveTrack, stepCount, stepsDone, type StepCount, type Track, type TrackRow } from "@/lib/buy/track";
import { cn } from "@/lib/utils";
import { DoneIllustration } from "./Illustrations";
import MicrosoftApproval from "./MicrosoftApproval";
import { waitingForApproval } from "@/lib/buy/approval";
import { EASE, HOVER_LIFT, PRESS_SPRING } from "@/lib/buy/motion";

const NODE = 28;

export default function SetupTrack({ order, loading, onBuyMore }: { order: Order | undefined; loading: boolean; onBuyMore?: () => void }) {
    const reduced = useReducedMotion();
    const track = deriveTrack(order);
    const done = track.phase === "done";
    const names = order?.domains.map((d) => d.name) ?? [];
    // Creating live mailboxes is the slow part: the provider can take hours, now and then a day or two.
    const creating = order?.steps.some((s) => s.key === "mailboxes" && s.status === "running") ?? false;
    const headline = loading && !order ? "Loading your order…" : track.headline;
    const activity = activityLine(track, names);
    return (
        <div>
            <Progress track={track} />
            {order && waitingForApproval(order).length > 0 && <MicrosoftApproval order={order} />}
            <AnimatePresence mode="wait" initial={false}>
                {done && order ? (
                    <motion.div key="done" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}>
                        <DoneMoment order={order} onBuyMore={onBuyMore} />
                    </motion.div>
                ) : (
                    <motion.div key="track" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0, y: -6 }}>
                        <Crossfade value={headline} className="mt-4 min-h-[28px]">
                            <h2 className="text-[20px] font-semibold tracking-[-0.02em] text-slate-900">{headline}</h2>
                        </Crossfade>
                        <span className="sr-only" aria-live="polite">
                            {headline}
                        </span>
                        <Crossfade value={activity} className="mt-1 min-h-[20px]">
                            <p className="text-[12.5px] text-slate-500 max-w-[60ch] leading-relaxed">{activity}</p>
                        </Crossfade>
                        <AnimatePresence initial={false}>
                            {creating && track.phase === "working" && (
                                <motion.div
                                    key="slow"
                                    initial={reduced ? { opacity: 0 } : { opacity: 0, height: 0 }}
                                    animate={{ opacity: 1, height: "auto" }}
                                    exit={reduced ? { opacity: 0 } : { opacity: 0, height: 0 }}
                                    className="overflow-hidden"
                                >
                                    <div className="mt-4 rounded-lg border border-sky-100 bg-sky-50/50 px-3.5 py-2.5 flex items-start gap-2.5">
                                        <ClockIcon className="w-4 h-4 text-sky-600 mt-px shrink-0" />
                                        <p className="text-[12px] text-sky-900 leading-relaxed">
                                            Setting up the mailboxes can take a few hours, and now and then a day or two. Nothing is needed from you. You can close
                                            this page, and Warmbly notifies you as soon as they are ready.
                                        </p>
                                    </div>
                                </motion.div>
                            )}
                        </AnimatePresence>
                        {(track.phase === "partial" || track.phase === "failed") && order && <FailureNote order={order} />}
                        <Rail track={track} order={order} />
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}

/** Swaps its child with a short crossfade whenever `value` changes. */
function Crossfade({ value, className, children }: { value: string; className?: string; children: React.ReactNode }) {
    const reduced = useReducedMotion();
    return (
        <div className={cn("relative", className)}>
            <AnimatePresence mode="popLayout" initial={false}>
                <motion.div
                    key={value}
                    initial={reduced ? { opacity: 0 } : { opacity: 0, y: 6, filter: "blur(2px)" }}
                    animate={{ opacity: 1, y: 0, filter: "blur(0px)" }}
                    exit={reduced ? { opacity: 0 } : { opacity: 0, y: -6, filter: "blur(2px)" }}
                    transition={{ duration: 0.28, ease: EASE }}
                >
                    {children}
                </motion.div>
            </AnimatePresence>
        </div>
    );
}

/** "3 of 6" with a bar that glides to it; green once everything is done. */
function Progress({ track }: { track: Track }) {
    const reduced = useReducedMotion();
    const total = track.rows.length;
    const finished = stepsDone(track);
    const done = track.phase === "done";
    const failed = track.phase === "failed" || track.phase === "partial";
    // A step under way counts for a little, so the bar never sits still while work is happening.
    const share = done ? 1 : (finished + (track.activeIndex >= 0 ? 0.35 : 0)) / total;
    return (
        <div className="flex items-center gap-3">
            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium shrink-0">Setup</span>
            <div
                role="progressbar"
                aria-label="Setup progress"
                aria-valuemin={0}
                aria-valuemax={total}
                aria-valuenow={finished}
                className="relative flex-1 h-1.5 rounded-full bg-slate-100 overflow-hidden"
            >
                <motion.span
                    className={cn("absolute inset-y-0 left-0 rounded-full", done ? "bg-emerald-500" : failed ? "bg-amber-400" : "bg-sky-500")}
                    initial={false}
                    animate={{ width: `${Math.round(share * 100)}%` }}
                    transition={reduced ? { duration: 0 } : { type: "spring", stiffness: 60, damping: 18 }}
                />
                {!done && !failed && !reduced && (
                    <span aria-hidden="true" className="absolute inset-y-0 -left-1/3 w-1/3 progress-sweep bg-gradient-to-r from-transparent via-white/70 to-transparent" />
                )}
            </div>
            <span className="text-[12px] text-slate-500 tabular-nums shrink-0">
                <span className="font-semibold text-slate-900">
                    <RollingNumber value={finished} />
                </span>{" "}
                of {total}
            </span>
        </div>
    );
}

function Rail({ track, order }: { track: Track; order: Order | undefined }) {
    const reduced = useReducedMotion();
    const { rows } = track;
    return (
        <ol className="relative mt-6" aria-label="Setup steps">
            {rows.map((r, i) => {
                const next = rows[i + 1];
                const fill = r.state === "done" || r.state === "skipped" ? (next && next.state !== "pending" ? 1 : 0.5) : 0;
                const flowing = (r.state === "done" || r.state === "skipped") && next?.state === "active";
                const count = r.state === "active" ? stepCount(order, r.key) : null;
                return (
                    <li key={r.key} className="relative flex gap-3.5 pb-5 last:pb-0">
                        {/* This row's piece of the rail, node centre to next node centre: full once the next step starts, half while this one wraps up. */}
                        {i < rows.length - 1 && (
                            <span aria-hidden="true" className="absolute w-0.5 rounded-full bg-slate-200 overflow-hidden" style={{ left: NODE / 2 - 1, top: NODE / 2, bottom: -NODE / 2 }}>
                                <motion.span
                                    className="absolute inset-0 bg-sky-500 origin-top"
                                    initial={false}
                                    animate={{ scaleY: fill }}
                                    transition={reduced ? { duration: 0 } : { duration: 0.7, ease: EASE, delay: 0.1 }}
                                />
                                {flowing && !reduced && (
                                    <motion.span
                                        className="absolute inset-x-0 h-1/3 bg-gradient-to-b from-transparent via-sky-200 to-transparent"
                                        initial={{ y: "-100%" }}
                                        animate={{ y: "300%" }}
                                        transition={{ duration: 1.3, repeat: Infinity, ease: "easeInOut" }}
                                    />
                                )}
                            </span>
                        )}
                        <Node state={r.state} index={i} />
                        <div className="min-w-0 flex-1 pt-[3px]">
                            <AnimatePresence mode="wait" initial={false}>
                                <motion.div
                                    key={r.title}
                                    initial={reduced ? false : { opacity: 0, y: 3 }}
                                    animate={{ opacity: 1, y: 0 }}
                                    exit={reduced ? undefined : { opacity: 0, y: -3 }}
                                    transition={{ duration: 0.18 }}
                                    className={cn(
                                        "text-[13px] leading-snug",
                                        r.state === "active" ? "text-slate-900 font-medium" : r.state === "done" ? "text-slate-800" : r.state === "failed" ? "text-red-700 font-medium" : "text-slate-400",
                                    )}
                                >
                                    {r.title}
                                    {r.state === "active" && <span className="ai-shimmer-text ml-1">…</span>}
                                </motion.div>
                            </AnimatePresence>
                            <div className={cn("text-[11.5px] leading-snug", r.state === "pending" ? "text-slate-300" : "text-slate-500")}>
                                {r.state === "done" && r.at ? `Done at ${r.at.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" })}` : r.hint}
                            </div>
                            <AnimatePresence initial={false}>{count && <Count key="count" c={count} />}</AnimatePresence>
                        </div>
                    </li>
                );
            })}
        </ol>
    );
}

/** "2 of 7 registered", rolling as each one lands, over a thin bar. */
function Count({ c }: { c: StepCount }) {
    const reduced = useReducedMotion();
    return (
        <motion.div
            initial={reduced ? { opacity: 0 } : { opacity: 0, height: 0 }}
            animate={{ opacity: 1, height: "auto" }}
            exit={reduced ? { opacity: 0 } : { opacity: 0, height: 0 }}
            transition={{ duration: 0.22, ease: EASE }}
            className="overflow-hidden"
        >
            <div className="pt-1.5 flex items-center gap-2 max-w-[260px]">
                <span className="relative flex-1 h-1 rounded-full bg-sky-100 overflow-hidden">
                    <motion.span
                        className="absolute inset-y-0 left-0 rounded-full bg-sky-500"
                        initial={false}
                        animate={{ width: `${c.total ? Math.round((c.done / c.total) * 100) : 0}%` }}
                        transition={reduced ? { duration: 0 } : { type: "spring", stiffness: 70, damping: 18 }}
                    />
                </span>
                <span className="text-[11px] text-slate-600 tabular-nums shrink-0">
                    <span className="font-semibold text-slate-900">
                        <RollingNumber value={c.done} />
                    </span>{" "}
                    of {c.total} {c.noun}
                </span>
            </div>
        </motion.div>
    );
}

function Node({ state, index }: { state: TrackRow["state"]; index: number }) {
    const reduced = useReducedMotion();
    return (
        <span className="relative shrink-0 inline-flex items-center justify-center" style={{ width: NODE, height: NODE }}>
            {state === "active" && !reduced && (
                <>
                    <motion.span
                        aria-hidden="true"
                        className="absolute inset-0 rounded-full ring-4 ring-sky-400/25"
                        animate={{ scale: [1, 1.18, 1], opacity: [0.9, 0.35, 0.9] }}
                        transition={{ duration: 1.8, repeat: Infinity, ease: "easeInOut" }}
                    />
                    <motion.span
                        aria-hidden="true"
                        className="absolute inset-0 rounded-full bg-sky-400/20"
                        initial={{ scale: 0.8, opacity: 0.7 }}
                        animate={{ scale: 1.7, opacity: 0 }}
                        transition={{ duration: 1.8, repeat: Infinity, ease: "easeOut" }}
                    />
                </>
            )}
            <motion.span
                key={state}
                initial={reduced || state !== "done" ? false : { scale: 0.6 }}
                animate={{ scale: state === "done" && !reduced ? [0.6, 1.15, 1] : 1 }}
                transition={{ duration: 0.42, ease: EASE, delay: state === "done" ? Math.min(index, 5) * 0.04 : 0 }}
                className={cn(
                    "relative size-7 rounded-full inline-flex items-center justify-center border-2 transition-colors duration-300",
                    state === "done" && "bg-sky-600 border-sky-600 text-white",
                    state === "active" && "bg-white border-sky-500",
                    state === "pending" && "bg-white border-slate-200",
                    state === "failed" && "bg-red-50 border-red-400 text-red-600",
                    state === "skipped" && "bg-slate-50 border-slate-200 text-slate-400",
                )}
            >
                {state === "done" && (
                    <svg viewBox="0 0 16 16" className="w-3.5 h-3.5" fill="none" aria-hidden="true">
                        <motion.path
                            d="M3.5 8.5l3 3 6-7"
                            stroke="currentColor"
                            strokeWidth="2.2"
                            strokeLinecap="round"
                            strokeLinejoin="round"
                            initial={reduced ? false : { pathLength: 0 }}
                            animate={{ pathLength: 1 }}
                            transition={{ duration: 0.38, ease: "easeOut", delay: 0.12 + Math.min(index, 5) * 0.04 }}
                        />
                    </svg>
                )}
                {state === "active" && (
                    <motion.span
                        className="size-2 rounded-full bg-sky-500"
                        animate={reduced ? undefined : { scale: [1, 0.7, 1] }}
                        transition={{ duration: 1.8, repeat: Infinity, ease: "easeInOut" }}
                    />
                )}
                {state === "failed" && <XIcon className="w-3.5 h-3.5" strokeWidth={2.5} />}
                {state === "skipped" && <MinusIcon className="w-3.5 h-3.5" />}
                <span className="sr-only">{state}</span>
            </motion.span>
        </span>
    );
}

function FailureNote({ order }: { order: Order }) {
    return (
        <div className="mt-4 rounded-lg border border-amber-200 bg-amber-50/60 px-3.5 py-2.5 flex items-start gap-2.5">
            <AlertTriangleIcon className="w-4 h-4 text-amber-600 mt-px shrink-0" />
            <div className="text-[12px] text-amber-900 leading-relaxed">
                {order.refunded_micros > 0 ? `${fmtMicros(order.refunded_micros)} went back to your balance. ` : ""}
                {order.mailboxes.some((m) => m.connect_state === "connected")
                    ? "The mailboxes that were created are connected and warming up below."
                    : "Nothing else was charged. You can try again with another domain."}
            </div>
        </div>
    );
}

function chip(m: StoreMailbox): { label: string; tone: string } {
    if (m.action_required === "microsoft_consent" && m.connect_state === "waiting") {
        return { label: "Needs approval", tone: "bg-sky-50 border-sky-200 text-sky-800" };
    }
    switch (m.connect_state) {
        case "connected":
            return { label: "Warming up", tone: "bg-emerald-50 border-emerald-100 text-emerald-700" };
        case "failed":
            return { label: "Needs attention", tone: "bg-amber-50 border-amber-200 text-amber-800" };
        default:
            return { label: "Connecting", tone: "bg-sky-50 border-sky-100 text-sky-700" };
    }
}

function DoneMoment({ order, onBuyMore }: { order: Order; onBuyMore?: () => void }) {
    const reduced = useReducedMotion();
    const count = order.mailboxes.length;
    const domains = order.domains.map((d) => d.name);
    const connected = order.mailboxes.filter((m) => m.connect_state === "connected").length;
    const stuck = order.mailboxes.filter((m) => m.connect_state === "failed").length;
    const allIn = count > 0 && connected === count;
    const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;
    return (
        <div className="flex flex-col items-center text-center">
            <DoneIllustration className="mt-2 w-[240px] max-w-full h-[170px]" />
            <motion.h2
                initial={reduced ? false : { opacity: 0, y: 6 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ delay: 0.35, duration: 0.3 }}
                className="mt-1 text-[22px] font-semibold tracking-[-0.02em] text-slate-900"
                aria-live="polite"
            >
                {count === 0 ? "Your domain is ready" : allIn ? `${plural(count, "mailbox is", "mailboxes are")} ready` : `${plural(count, "mailbox", "mailboxes")} created`}
            </motion.h2>
            <motion.p
                initial={reduced ? false : { opacity: 0 }}
                animate={{ opacity: 1 }}
                transition={{ delay: 0.45 }}
                className="mt-1 text-[12.5px] text-slate-500 max-w-[52ch] leading-relaxed"
            >
                {domains.join(", ")} {domains.length === 1 ? "is" : "are"} registered and authenticated.{" "}
                {count === 0
                    ? "Add mailboxes on it whenever you like."
                    : allIn
                      ? "Every mailbox is connected and warmup has started. It ramps up slowly over the next few weeks."
                      : stuck > 0
                        ? `${plural(stuck, "mailbox", "mailboxes")} could not connect to Warmbly yet. Warmbly retries on its own; the rest start warming up as soon as they connect.`
                        : "Connecting the mailboxes to Warmbly now. Warmup starts on each one as soon as it connects."}
            </motion.p>

            {count > 0 && (
                <motion.ul
                    initial="hidden"
                    animate="show"
                    variants={{ hidden: {}, show: { transition: { staggerChildren: reduced ? 0 : 0.05, delayChildren: reduced ? 0 : 0.5 } } }}
                    className="mt-5 w-full max-w-[460px] rounded-lg border border-slate-200 divide-y divide-slate-100 bg-white text-left overflow-hidden"
                >
                    {order.mailboxes.map((m) => {
                        const c = chip(m);
                        return (
                            <motion.li
                                key={m.id}
                                variants={{ hidden: { opacity: 0, y: reduced ? 0 : 6 }, show: { opacity: 1, y: 0 } }}
                                className="px-3 py-2 flex items-center gap-2.5"
                            >
                                <ProviderLogo id={m.provider} size="md" />
                                <span className="min-w-0 flex-1">
                                    <span className="block text-[12.5px] text-slate-900 truncate">{m.email}</span>
                                    {(m.first_name || m.last_name) && (
                                        <span className="block text-[11px] text-slate-500 truncate">{[m.first_name, m.last_name].filter(Boolean).join(" ")}</span>
                                    )}
                                </span>
                                <span className={cn("h-[18px] px-1.5 rounded-full border text-[10.5px] font-medium inline-flex items-center gap-1 shrink-0", c.tone)}>
                                    {m.connect_state === "connected" && <span className="size-1.5 rounded-full bg-emerald-500" />}
                                    {c.label}
                                </span>
                            </motion.li>
                        );
                    })}
                </motion.ul>
            )}

            <motion.div
                initial={reduced ? false : { opacity: 0, y: 6 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ delay: 0.7, duration: 0.3 }}
                className="mt-5 flex flex-wrap items-center justify-center gap-2"
            >
                <motion.span whileHover={reduced ? undefined : HOVER_LIFT} whileTap={reduced ? undefined : { scale: 0.97 }} transition={PRESS_SPRING} className="inline-flex rounded-lg">
                    <Link
                        to="/app/emails"
                        className="h-9 px-3.5 rounded-lg bg-sky-600 hover:bg-sky-700 text-white text-[12.5px] font-semibold inline-flex items-center gap-1.5 transition-colors outline-none focus-visible:ring-2 focus-visible:ring-sky-300 focus-visible:ring-offset-2"
                    >
                        Go to mailboxes <ArrowRightIcon className="w-3.5 h-3.5" />
                    </Link>
                </motion.span>
                <motion.span whileHover={reduced ? undefined : HOVER_LIFT} whileTap={reduced ? undefined : { scale: 0.97 }} transition={PRESS_SPRING} className="inline-flex rounded-lg">
                    <Link
                        to="/app/campaigns"
                        className="h-9 px-3.5 rounded-lg border border-slate-200 hover:border-slate-300 bg-white text-slate-800 text-[12.5px] font-medium inline-flex items-center gap-1.5 transition-colors outline-none focus-visible:ring-2 focus-visible:ring-sky-200"
                    >
                        <SendIcon className="w-3.5 h-3.5 text-slate-500" /> Start a campaign
                    </Link>
                </motion.span>
            </motion.div>
            {onBuyMore && (
                <button type="button" onClick={onBuyMore} className="mt-3 text-[12px] text-slate-500 hover:text-slate-800 transition-colors">
                    Buy more domains
                </button>
            )}
        </div>
    );
}

/** The same track in one line, for a list row: a segment per step and what is happening now. */
export function CompactTrack({ order, className }: { order: Order; className?: string }) {
    const reduced = useReducedMotion();
    const track = deriveTrack(order);
    return (
        <div className={cn("min-w-0", className)}>
            <ol className="flex items-center gap-1" aria-label="Setup steps">
                {track.rows.map((r) => (
                    <li
                        key={r.key}
                        title={r.title}
                        className={cn(
                            "relative h-1 flex-1 rounded-full overflow-hidden transition-colors duration-300",
                            r.state === "done" && "bg-sky-500",
                            r.state === "skipped" && "bg-slate-300",
                            r.state === "failed" && "bg-red-400",
                            r.state === "active" && "bg-sky-100",
                            r.state === "pending" && "bg-slate-200",
                        )}
                    >
                        {r.state === "active" && !reduced && (
                            <motion.span
                                aria-hidden="true"
                                className="absolute inset-y-0 w-1/2 rounded-full bg-sky-400"
                                initial={{ x: "-100%" }}
                                animate={{ x: "200%" }}
                                transition={{ duration: 1.4, repeat: Infinity, ease: "easeInOut" }}
                            />
                        )}
                        <span className="sr-only">
                            {r.title}: {r.state}
                        </span>
                    </li>
                ))}
            </ol>
            <AnimatePresence mode="wait" initial={false}>
                <motion.p
                    key={track.headline}
                    initial={reduced ? false : { opacity: 0, y: 2 }}
                    animate={{ opacity: 1, y: 0 }}
                    exit={reduced ? undefined : { opacity: 0, y: -2 }}
                    transition={{ duration: 0.16 }}
                    aria-live="polite"
                    className="mt-1 text-[11.5px] text-slate-500 truncate"
                >
                    {track.headline}
                </motion.p>
            </AnimatePresence>
        </div>
    );
}
