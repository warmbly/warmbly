// Tour scenes. Each one is the real dashboard (the guides' screenshots of the
// "Sunrise Labs" sample workspace) framed on the part the slide talks about,
// with one spot lit. Remie and Slack, which have no screenshot, are drawn over
// them in fixed light colours so dark mode cannot repaint them.

import type { ComponentType, ReactNode } from "react";
import { motion, useReducedMotion } from "framer-motion";
import { CheckIcon, HashIcon, ListTodoIcon, PauseIcon, SparklesIcon, XIcon } from "lucide-react";
import AgentMark from "@/components/app/agent/AgentMark";
import { EASE, SCENE_W, useLoop, useSceneHeight } from "./sceneTokens";
import type { SceneId } from "./tourSteps";
import {
    accounts,
    addAccount,
    analytics,
    automationBuilder,
    automations,
    avatar,
    campaignOverview,
    campaignSteps,
    campaigns,
    contacts,
    deals,
    deliverability,
    mailboxWarmup,
    sendingDomains,
    unibox,
} from "./tourShots";

const FLOAT = "absolute rounded-[12px] bg-[#fff] ring-1 ring-[#0f172a]/[0.08] shadow-[0_24px_60px_-26px_rgba(15,23,42,0.55)]";

type Box = { x: number; y: number; w: number; h: number };
type Spot = Box & { label: string; side?: "below" | "above" | "left" | "right" };

/** Everything right of the sidebar and below the top bar, in screenshot pixels. */
const MAIN: Box = { x: 286, y: 64, w: 1314, h: 560 };

/**
 * A screenshot scaled so `frame` fits the canvas (never leaving it uncovered),
 * drifting slowly, with one spot lit. `overlay` is drawn in screenshot pixels.
 */
function Shot({
    src,
    frame = MAIN,
    size = [1600, 1000],
    spot,
    overlay,
    children,
}: {
    src: string;
    frame?: Box;
    size?: [number, number];
    spot?: Spot;
    overlay?: ReactNode;
    children?: ReactNode;
}) {
    const reduced = useReducedMotion();
    const H = useSceneHeight();
    const [iw, ih] = size;
    const s = Math.max(SCENE_W / iw, H / ih, Math.min(SCENE_W / frame.w, H / frame.h));
    const clamp = (v: number, min: number) => Math.min(0, Math.max(min, v));
    const left = clamp(-frame.x * s + Math.max(0, (SCENE_W - frame.w * s) / 2), SCENE_W - iw * s);
    const top = clamp(-frame.y * s, H - ih * s);
    const room = ih * s + top - H;
    const pan = reduced ? 0 : Math.max(0, Math.min(18, room));
    return (
        <div className="absolute inset-0 overflow-hidden bg-[#f8fafc]">
            <motion.div
                className="absolute"
                style={{ left, top, width: iw * s, height: ih * s }}
                animate={pan ? { y: [0, -pan, 0] } : undefined}
                transition={{ duration: 18, repeat: Infinity, ease: "easeInOut" }}
            >
                <img src={src} alt="" draggable={false} decoding="async" className="block h-full w-full select-none" />
                {overlay && (
                    <div className="absolute left-0 top-0 origin-top-left" style={{ width: iw, height: ih, transform: `scale(${s})` }}>
                        {overlay}
                    </div>
                )}
                {spot && <Highlight spot={spot} scale={s} />}
            </motion.div>
            {children}
        </div>
    );
}

const LABEL_SIDE = {
    below: "left-0 top-full mt-2.5",
    above: "left-0 bottom-full mb-2.5",
    left: "right-full mr-3 top-0",
    right: "left-full ml-3 top-0",
};

function Highlight({ spot, scale }: { spot: Spot; scale: number }) {
    const reduced = useReducedMotion();
    return (
        <motion.div
            className="absolute"
            style={{ left: spot.x * scale, top: spot.y * scale, width: spot.w * scale, height: spot.h * scale }}
            initial={reduced ? false : { opacity: 0, scale: 1.06 }}
            animate={{ opacity: 1, scale: 1 }}
            transition={{ duration: 0.6, delay: 0.5, ease: EASE }}
        >
            <span className="absolute -inset-1 rounded-[8px] ring-2 ring-[#0ea5e9] bg-[#0ea5e9]/[0.06]" />
            {!reduced && (
                <motion.span
                    className="absolute -inset-1 rounded-[8px] ring-2 ring-[#0ea5e9]"
                    animate={{ opacity: [0.7, 0], scale: [1, 1.08] }}
                    transition={{ duration: 1.8, repeat: Infinity, ease: "easeOut", delay: 1.1 }}
                />
            )}
            <span
                className={
                    "absolute whitespace-nowrap rounded-full bg-[#0f172a] px-3 h-7 inline-flex items-center text-[12px] font-medium text-[#fff] shadow-[0_8px_20px_-8px_rgba(15,23,42,0.6)] " +
                    LABEL_SIDE[spot.side ?? "below"]
                }
            >
                {spot.label}
            </span>
        </motion.div>
    );
}

function Reveal({ delay, children, className }: { delay: number; children: ReactNode; className?: string }) {
    const reduced = useReducedMotion();
    return (
        <motion.div
            className={className}
            initial={reduced ? false : { opacity: 0, y: 5 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.45, delay, ease: EASE }}
        >
            {children}
        </motion.div>
    );
}

/* ── Welcome: the whole dashboard, with Remie and Slack beside it ─── */

function WelcomeScene() {
    const reduced = useReducedMotion();
    const bob = (delay: number) =>
        reduced ? undefined : { y: [0, -6, 0], transition: { duration: 6, delay, repeat: Infinity, ease: "easeInOut" as const } };
    return (
        <Shot src={accounts} frame={{ x: 0, y: 0, w: 1600, h: 800 }}>
            <motion.div
                className={`${FLOAT} right-6 top-14 w-[300px] p-3.5`}
                initial={reduced ? false : { opacity: 0, y: 12 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ duration: 0.6, delay: 0.4, ease: EASE }}
            >
                <motion.div animate={bob(0)}>
                    <RemieHeader />
                    <div className="mt-2.5 ml-auto w-fit max-w-[240px] rounded-[10px] rounded-br-sm bg-[#f1f5f9] px-2.5 py-1.5 text-[12px] leading-[1.45] text-[#1e293b]">
                        Which mailboxes need attention?
                    </div>
                    <div className="mt-2 text-[12px] leading-[1.5] text-[#475569]">
                        None right now. 19 are still warming up, and <span className="font-medium text-[#0f172a]">elena.voss</span> is on day 57 of its ramp.
                    </div>
                </motion.div>
            </motion.div>
            <motion.div
                className={`${FLOAT} left-[200px] bottom-6 w-[340px] p-3.5`}
                initial={reduced ? false : { opacity: 0, y: 12 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ duration: 0.6, delay: 0.8, ease: EASE }}
            >
                <motion.div animate={bob(1.4)} className="flex gap-2.5">
                    <img src={avatar} alt="" className="size-9 shrink-0 rounded-[6px]" />
                    <div className="min-w-0">
                        <SlackName name="Warmbly" app time="9:42 AM" />
                        <div className="text-[12.5px] leading-[1.45] text-[#1d1c1d]">
                            New reply from <b>Eli Grant</b>: interested, and asking for the deliverability benchmarks.
                        </div>
                    </div>
                </motion.div>
            </motion.div>
        </Shot>
    );
}

/* ── Mailboxes ────────────────────────────────────────────────────── */

function AddAccountScene() {
    return (
        <Shot
            src={addAccount}
            frame={{ x: 400, y: 270, w: 800, h: 500 }}
            spot={{ x: 489, y: 637, w: 621, h: 72, label: "Or bring them from an inbox vendor" }}
        />
    );
}

function AccountsScene() {
    return <Shot src={accounts} spot={{ x: 1146, y: 342, w: 98, h: 374, label: "Each mailbox's warmup, ramping daily", side: "left" }} />;
}

// The warmup tab slides in over the list, as it does in the dashboard, and scrolls to the ramp.
function WarmupScene() {
    const reduced = useReducedMotion();
    const H = useSceneHeight();
    const w = 360;
    const tall = (1120 * w) / 900;
    const scroll = Math.max(0, tall - H);
    return (
        <Shot src={accounts}>
            <div className="absolute inset-0 bg-[#0f172a]/[0.18]" />
            <motion.div
                className="absolute right-0 top-0 bottom-0 overflow-hidden bg-[#fff] shadow-[-24px_0_60px_-30px_rgba(15,23,42,0.6)]"
                style={{ width: w }}
                initial={reduced ? false : { x: 40, opacity: 0 }}
                animate={{ x: 0, opacity: 1 }}
                transition={{ duration: 0.6, ease: EASE }}
            >
                <motion.img
                    src={mailboxWarmup}
                    alt=""
                    draggable={false}
                    className="block w-full select-none"
                    animate={reduced || !scroll ? undefined : { y: [0, 0, -scroll, -scroll, 0] }}
                    transition={{ duration: 14, times: [0, 0.2, 0.5, 0.8, 1], repeat: Infinity, ease: "easeInOut" }}
                />
            </motion.div>
        </Shot>
    );
}

function DomainsScene() {
    return (
        <Shot
            src={sendingDomains}
            frame={{ ...MAIN, h: 440 }}
            spot={{ x: 800, y: 336, w: 400, h: 134, label: "SPF, DKIM, DMARC and your own tracking domain" }}
        />
    );
}

/* ── Campaigns ────────────────────────────────────────────────────── */

function CampaignListScene() {
    return <Shot src={campaigns} frame={{ ...MAIN, h: 500 }} spot={{ x: 1316, y: 280, w: 80, h: 275, label: "Running, paused, draft or done", side: "left" }} />;
}

function CampaignStepsScene() {
    return <Shot src={campaignSteps} frame={{ ...MAIN, w: 1067 }} spot={{ x: 312, y: 254, w: 146, h: 32, label: "Stops for anyone who replies", side: "right" }} />;
}

function CampaignLiveScene() {
    return <Shot src={campaignOverview} frame={{ ...MAIN, h: 500 }} spot={{ x: 308, y: 248, w: 1270, h: 52, label: "What went out today, and why" }} />;
}

/* ── Contacts ─────────────────────────────────────────────────────── */

function ContactsScene() {
    return <Shot src={contacts} spot={{ x: 1146, y: 120, w: 210, h: 32, label: "Import a file, or sync a Google Sheet", side: "left" }} />;
}

function SegmentsScene() {
    return <Shot src={contacts} spot={{ x: 300, y: 68, w: 480, h: 36, label: "Segments, labels and the suppression list" }} />;
}

/* ── Inbox ────────────────────────────────────────────────────────── */

function InboxScene() {
    return <Shot src={unibox} frame={{ ...MAIN, h: 600 }} spot={{ x: 862, y: 306, w: 68, h: 28, label: "Labelled, from every mailbox" }} />;
}

const DRAFT = "Hi Eli, happy to. The benchmarks are attached, and I can walk you through them. Does Thursday at 2pm work?";

// A suggested reply waiting in the reading pane, as the inbox agent leaves it.
function InboxDraftsScene() {
    const reduced = useReducedMotion();
    const loop = useLoop(12000);
    const words = DRAFT.split(" ");
    return (
        <Shot
            src={unibox}
            frame={{ x: 540, y: 64, w: 1060, h: 520 }}
            overlay={
                <Reveal key={loop} delay={0.4} className="absolute left-[966px] top-[262px] w-[612px] rounded-[12px] bg-[#fff] ring-1 ring-[#bae6fd] shadow-[0_24px_60px_-30px_rgba(2,132,199,0.5)]">
                    <div className="flex h-11 items-center gap-2 border-b border-[#e0f2fe] px-4 text-[13px] font-medium text-[#0369a1]">
                        <SparklesIcon className="size-4" /> Agent draft
                        <span className="ml-auto text-[12px] font-normal text-[#94a3b8]">from marcus.reid@sunriselabs.io</span>
                    </div>
                    <p className="px-4 py-3.5 text-[15px] leading-[1.6] text-[#1e293b]">
                        {words.map((w, i) => (
                            <motion.span
                                key={i}
                                initial={reduced ? false : { opacity: 0 }}
                                animate={{ opacity: 1 }}
                                transition={{ duration: 0.12, delay: 1 + i * 0.06 }}
                            >
                                {w}{" "}
                            </motion.span>
                        ))}
                    </p>
                    <div className="flex gap-2 px-4 pb-4">
                        <span className="h-9 px-4 rounded-md bg-[#0284c7] text-[13px] font-medium text-[#fff] inline-flex items-center">Send</span>
                        <span className="h-9 px-4 rounded-md bg-[#fff] text-[13px] font-medium text-[#334155] ring-1 ring-[#e2e8f0] inline-flex items-center">Edit</span>
                        <span className="h-9 px-4 rounded-md bg-[#fff] text-[13px] font-medium text-[#64748b] ring-1 ring-[#e2e8f0] inline-flex items-center">Discard</span>
                    </div>
                </Reveal>
            }
            spot={{ x: 966, y: 262, w: 612, h: 200, label: "Sent only when you say so", side: "left" }}
        />
    );
}

/* ── Analytics, deliverability, CRM, automations ──────────────────── */

function AnalyticsScene() {
    return <Shot src={analytics} frame={{ ...MAIN, y: 400, h: 470 }} spot={{ x: 290, y: 660, w: 1306, h: 194, label: "Where your replies really come from", side: "above" }} />;
}

function DeliverabilityScene() {
    return <Shot src={deliverability} spot={{ x: 290, y: 120, w: 255, h: 104, label: "One score for bounces, complaints and placement" }} />;
}

function DealsScene() {
    return <Shot src={deals} frame={{ ...MAIN, h: 520 }} spot={{ x: 615, y: 118, w: 326, h: 112, label: "Open pipeline, at a glance" }} />;
}

function AutomationListScene() {
    return <Shot src={automations} frame={{ ...MAIN, y: 260, h: 580 }} spot={{ x: 290, y: 700, w: 1306, h: 136, label: "Start from a template", side: "above" }} />;
}

function AutomationBuilderScene() {
    return (
        <Shot src={automationBuilder} frame={{ x: 480, y: 64, w: 1067, h: 560 }} spot={{ x: 800, y: 298, w: 292, h: 62, label: "Branches on what the reply says", side: "right" }} />
    );
}

/* ── Remie ────────────────────────────────────────────────────────── */

function RemieHeader() {
    return (
        <div className="flex items-center gap-2">
            <AgentMark size={18} />
            <span className="text-[13px] font-medium text-[#0f172a]">Remie</span>
        </div>
    );
}

function Steps({ items, from }: { items: string[]; from: number }) {
    return (
        <div className="space-y-1.5">
            {items.map((s, i) => (
                <Reveal key={s} delay={from + i * 0.5}>
                    <div className="flex items-center gap-2 text-[11.5px] text-[#64748b]">
                        <span className="size-4 rounded-full bg-[#ecfdf5] text-[#059669] inline-flex items-center justify-center">
                            <CheckIcon className="size-2.5" strokeWidth={3} />
                        </span>
                        {s}
                    </div>
                </Reveal>
            ))}
        </div>
    );
}

function Asked({ children }: { children: ReactNode }) {
    return (
        <Reveal delay={0.2} className="flex justify-end">
            <div className="max-w-[270px] rounded-[10px] rounded-br-sm bg-[#f1f5f9] px-3 py-2 text-[12px] leading-[1.45] text-[#1e293b]">{children}</div>
        </Reveal>
    );
}

function RemiePanel({ context, children }: { context: string; children: ReactNode }) {
    const loop = useLoop(14000);
    return (
        <div key={loop} className={`${FLOAT} right-3 top-3 bottom-3 w-[350px] flex flex-col overflow-hidden`}>
            <div className="h-11 shrink-0 px-4 border-b border-[#e2e8f0] flex items-center gap-2">
                <AgentMark size={18} state="thinking" />
                <span className="text-[13px] font-medium text-[#0f172a]">Remie</span>
                <span className="ml-auto text-[11px] text-[#94a3b8]">{context}</span>
            </div>
            <div className="flex-1 min-h-0 overflow-hidden px-4 py-3 space-y-3">{children}</div>
            <div className="m-3 h-9 shrink-0 rounded-[8px] ring-1 ring-[#e2e8f0] px-3 flex items-center text-[11.5px] text-[#94a3b8]">Ask Remie anything…</div>
        </div>
    );
}

const REPLY_RATES = [
    ["Agency partnerships", 14.3],
    ["Spring product launch", 6.7],
    ["Sunrise Q3 launch outreach", 2.2],
    ["Feature announcement nudge", 0],
] as const;

function RemieAskScene() {
    const reduced = useReducedMotion();
    return (
        <Shot src={analytics} frame={{ x: 286, y: 64, w: 900, h: 600 }}>
            <div className="absolute inset-0 bg-[#0f172a]/[0.04]" />
            <RemiePanel context="on Analytics">
                <Asked>Which campaign gets the most replies, and which one is struggling?</Asked>
                <Steps items={["Read 7 days of analytics", "Compared 4 campaigns"]} from={0.9} />
                <Reveal delay={2.1} className="space-y-1.5">
                    {REPLY_RATES.map(([name, rate], i) => (
                        <div key={name} className="flex items-center gap-2 text-[11px]">
                            <span className="w-[130px] truncate text-[#334155]">{name}</span>
                            <span className="h-2 flex-1 rounded-full bg-[#f1f5f9] overflow-hidden">
                                <motion.span
                                    className="block h-full rounded-full bg-[#0ea5e9]"
                                    initial={reduced ? false : { width: 0 }}
                                    animate={{ width: `${(rate / 14.3) * 100}%` }}
                                    transition={{ duration: 0.8, delay: 2.3 + i * 0.12, ease: EASE }}
                                />
                            </span>
                            <span className="w-9 text-right tabular-nums text-[#0f172a]">{rate.toFixed(1)}%</span>
                        </div>
                    ))}
                </Reveal>
                <Reveal delay={3.2}>
                    <p className="text-[12px] leading-[1.55] text-[#334155]">
                        <span className="font-medium text-[#0f172a]">Agency partnerships</span> leads, with 14.3% of its 14 sends answered.{" "}
                        <span className="font-medium text-[#0f172a]">Feature announcement nudge</span> has no replies from 4, so its opener is the one to rewrite.
                    </p>
                </Reveal>
                <Reveal delay={3.9} className="flex flex-wrap gap-1.5">
                    {["Rewrite that opener", "Show me the replies"].map((c) => (
                        <span key={c} className="h-7 px-2.5 rounded-full bg-[#fff] text-[11px] text-[#334155] ring-1 ring-[#e2e8f0] inline-flex items-center">
                            {c}
                        </span>
                    ))}
                </Reveal>
            </RemiePanel>
        </Shot>
    );
}

function RemieActScene() {
    return (
        <Shot src={campaignOverview} frame={{ x: 286, y: 64, w: 900, h: 600 }}>
            <div className="absolute inset-0 bg-[#0f172a]/[0.04]" />
            <RemiePanel context="on Sunrise Q3 launch outreach">
                <Asked>How is this doing? Pause anything that hurts deliverability.</Asked>
                <Steps items={["Read the campaign's numbers", "Checked the Advisor", "Compared 3 active campaigns"]} from={0.9} />
                <Reveal delay={2.6}>
                    <p className="text-[12px] leading-[1.55] text-[#334155]">
                        It opens at 94.5%, but <span className="font-medium text-[#0f172a]">5.5%</span> bounce, which hurts the mailboxes it sends from. I would pause it and clean the list first.
                    </p>
                </Reveal>
                <Reveal delay={3.3}>
                    <div className="rounded-[10px] bg-[#fffbeb] p-3 ring-1 ring-[#fde68a]">
                        <div className="flex items-center gap-1.5 text-[12px] font-medium text-[#0f172a]">
                            <PauseIcon className="size-3.5 text-[#d97706]" /> Pause Sunrise Q3 launch outreach?
                        </div>
                        <div className="mt-2.5 flex gap-1.5">
                            <span className="h-7 px-3 rounded-md bg-[#0284c7] text-[11.5px] font-medium text-[#fff] inline-flex items-center">Approve</span>
                            <span className="h-7 px-3 rounded-md bg-[#fff] text-[11.5px] font-medium text-[#334155] ring-1 ring-[#e2e8f0] inline-flex items-center">Skip</span>
                        </div>
                    </div>
                </Reveal>
            </RemiePanel>
        </Shot>
    );
}

/* ── Slack, in Slack's own colours ────────────────────────────────── */

function SlackName({ name, app = false, time }: { name: string; app?: boolean; time: string }) {
    return (
        <div className="flex items-baseline gap-1.5">
            <span className="text-[13px] font-bold text-[#1d1c1d]">{name}</span>
            {app && <span className="rounded-[3px] bg-[#e8e8e8] px-1 text-[9px] font-semibold text-[#616061]">APP</span>}
            <span className="text-[10.5px] text-[#616061]">{time}</span>
        </div>
    );
}

function Maya({ size = 9 }: { size?: 7 | 9 }) {
    return (
        <span
            className={
                "flex shrink-0 items-center justify-center rounded-[6px] bg-[#fde68a] font-bold text-[#92400e] " +
                (size === 9 ? "size-9 text-[11px]" : "size-7 text-[10px]")
            }
        >
            MO
        </span>
    );
}

function Mention({ children = "@Warmbly" }: { children?: ReactNode }) {
    return <span className="rounded-[3px] bg-[#e8f5fa] px-0.5 text-[#1264a3]">{children}</span>;
}

function SlackButton({ children, primary = false }: { children: ReactNode; primary?: boolean }) {
    return (
        <span
            className={
                "h-7 px-2.5 rounded-[4px] text-[11.5px] font-semibold inline-flex items-center " +
                (primary ? "bg-[#007a5a] text-[#fff]" : "border border-[#bbbbbb] bg-[#fff] text-[#1d1c1d]")
            }
        >
            {children}
        </span>
    );
}

function SlackShell({ active, children }: { active: string; children: ReactNode }) {
    return (
        <div className="absolute inset-0 flex bg-[#fff] font-[system-ui]">
            <aside className="w-[170px] shrink-0 bg-[#3f0e40] px-2 py-3 text-[12.5px] text-[#ffffffb3]">
                <div className="px-2 pb-3 text-[14px] font-bold text-[#fff]">Sunrise Labs</div>
                <div className="px-2 pb-1 text-[11px] text-[#ffffff80]">Channels</div>
                {["general", "sales", "inbox-replies", "random"].map((c) => (
                    <div key={c} className={"flex h-7 items-center gap-1.5 rounded-[5px] px-2 " + (c === active ? "bg-[#1164a3] text-[#fff]" : "")}>
                        <HashIcon className="size-3.5 opacity-70" />
                        {c}
                    </div>
                ))}
                <div className="mt-3 px-2 pb-1 text-[11px] text-[#ffffff80]">Apps</div>
                <div className={"flex h-7 items-center gap-1.5 rounded-[5px] px-2 " + (active === "Warmbly" ? "bg-[#1164a3] text-[#fff]" : "")}>
                    <img src={avatar} alt="" className="size-4 rounded-[3px]" />
                    Warmbly
                </div>
            </aside>
            {children}
        </div>
    );
}

// A direct message: a question about the week, then a change that waits for approval.
function SlackAskScene() {
    const loop = useLoop(15000);
    return (
        <SlackShell active="Warmbly">
            <div key={loop} className="flex min-w-0 flex-1 flex-col">
                <div className="flex h-11 shrink-0 items-center gap-2 border-b border-[#dddddd] px-4 text-[14px] font-bold text-[#1d1c1d]">
                    <img src={avatar} alt="" className="size-5 rounded-[4px]" />
                    Warmbly
                </div>
                <div className="flex min-h-0 flex-1 flex-col justify-end gap-3.5 overflow-hidden px-4 py-3.5">
                    <Reveal delay={0.3} className="flex gap-2.5">
                        <Maya />
                        <div className="min-w-0">
                            <SlackName name="Maya Okonkwo" time="9:40 AM" />
                            <div className="text-[12.5px] text-[#1d1c1d]">how did outreach do this week?</div>
                        </div>
                    </Reveal>
                    <Reveal delay={1.3} className="flex gap-2.5">
                        <img src={avatar} alt="" className="size-9 shrink-0 rounded-[6px]" />
                        <div className="min-w-0 text-[12.5px] leading-[1.5] text-[#1d1c1d]">
                            <SlackName name="Warmbly" app time="9:40 AM" />
                            <div>
                                <b>78</b> emails sent in the last 7 days. <b>92.3%</b> opened and <b>5.1%</b> replied.
                            </div>
                            <ul className="mt-1 list-disc pl-5">
                                <li>Agency partnerships is your best, at 14.3% replied</li>
                                <li>Feature announcement nudge has no replies from 4 sends</li>
                                <li>4 open deals worth $73,000, one of them Eli Grant at Arbor Security</li>
                            </ul>
                        </div>
                    </Reveal>
                    <Reveal delay={3} className="flex gap-2.5">
                        <Maya />
                        <div className="min-w-0">
                            <SlackName name="Maya Okonkwo" time="9:41 AM" />
                            <div className="text-[12.5px] text-[#1d1c1d]">add a task to follow up with Eli on Thursday</div>
                        </div>
                    </Reveal>
                    <Reveal delay={4} className="flex gap-2.5">
                        <img src={avatar} alt="" className="size-9 shrink-0 rounded-[6px]" />
                        <div className="min-w-0">
                            <SlackName name="Warmbly" app time="9:41 AM" />
                            <div className="mt-1 w-[330px] rounded-[6px] border border-[#dddddd] p-2.5">
                                <div className="flex items-center gap-1.5 text-[12.5px] font-bold text-[#1d1c1d]">
                                    <ListTodoIcon className="size-3.5" /> Create a task
                                </div>
                                <div className="mt-1 text-[11.5px] leading-[1.5] text-[#616061]">
                                    Follow up with Eli Grant · due Thursday · deal Arbor Security
                                </div>
                                <div className="mt-2 flex gap-1.5">
                                    <SlackButton primary>Approve</SlackButton>
                                    <SlackButton>Deny</SlackButton>
                                </div>
                            </div>
                        </div>
                    </Reveal>
                </div>
                <div className="mx-4 mb-3 h-9 shrink-0 rounded-[8px] border border-[#bbbbbb] px-3 flex items-center text-[12px] text-[#868686]">Message Warmbly</div>
            </div>
        </SlackShell>
    );
}

const FIELDS = [
    ["Mailbox", "marcus.reid@sunriselabs.io"],
    ["Campaign", "Sunrise Q3 launch outreach"],
    ["Intent", "positive"],
    ["Label", "Lead"],
];
const BUTTONS = ["Draft with AI", "Interested", "Not interested", "Assign to me", "Open in Warmbly"];

// The inbox channel: one post per reply, worked in its thread.
function SlackInboxScene() {
    const reduced = useReducedMotion();
    const loop = useLoop(13000);
    const words = DRAFT.split(" ");
    return (
        <SlackShell active="inbox-replies">
            <div className="flex min-w-0 flex-1 flex-col">
                <div className="flex h-11 shrink-0 items-center gap-1 border-b border-[#dddddd] px-4 text-[14px] font-bold text-[#1d1c1d]">
                    <HashIcon className="size-4 text-[#616061]" />
                    inbox-replies
                </div>
                <div className="flex gap-2.5 px-4 py-3.5">
                    <img src={avatar} alt="" className="size-9 shrink-0 rounded-[6px]" />
                    <div className="min-w-0 flex-1">
                        <SlackName name="Warmbly" app time="9:41 AM" />
                        <div className="text-[12.5px] text-[#1d1c1d]">
                            New reply from <b>Eli Grant</b>
                        </div>
                        <div className="mt-1.5 grid grid-cols-2 gap-x-4 gap-y-1">
                            {FIELDS.map(([k, v]) => (
                                <div key={k} className="min-w-0">
                                    <div className="text-[11px] font-bold text-[#1d1c1d]">{k}</div>
                                    <div className="truncate text-[11px] text-[#1d1c1d]">{v}</div>
                                </div>
                            ))}
                        </div>
                        <div className="mt-2 border-l-[3px] border-[#dddddd] pl-2.5 text-[12px] leading-[1.45] text-[#1d1c1d]">
                            Interested. Can you send the deliverability benchmarks you mentioned? Our current provider has been rough.
                        </div>
                        <div className="mt-2 flex flex-wrap gap-1">
                            <SlackButton primary>Reply</SlackButton>
                            {BUTTONS.map((b) => (
                                <SlackButton key={b}>{b}</SlackButton>
                            ))}
                        </div>
                        <div className="mt-2 inline-flex items-center gap-1.5 text-[11px]">
                            <span className="flex size-4 items-center justify-center rounded-[3px] bg-[#fde68a] text-[7.5px] font-bold text-[#92400e]">MO</span>
                            <span className="font-semibold text-[#1264a3]">2 replies</span>
                        </div>
                    </div>
                </div>
            </div>

            {/* The thread pane: asked in plain words, answered with a draft waiting for review. */}
            <div key={loop} className="flex w-[290px] shrink-0 flex-col border-l border-[#dddddd] bg-[#fff]">
                <div className="flex h-11 shrink-0 items-center gap-1.5 border-b border-[#dddddd] px-3.5 text-[13px] font-bold text-[#1d1c1d]">
                    Thread <span className="font-normal text-[#616061]"># inbox-replies</span>
                    <XIcon className="ml-auto size-4 text-[#616061]" />
                </div>
                <div className="min-h-0 flex-1 space-y-3 overflow-hidden px-3.5 py-3">
                    <Reveal delay={0.3} className="flex gap-2">
                        <Maya size={7} />
                        <div className="min-w-0">
                            <SlackName name="Maya Okonkwo" time="9:42 AM" />
                            <div className="text-[12px] leading-[1.45] text-[#1d1c1d]">
                                <Mention /> draft a reply with the benchmarks and offer Thursday
                            </div>
                        </div>
                    </Reveal>
                    <Reveal delay={1.3} className="flex gap-2">
                        <img src={avatar} alt="" className="size-7 shrink-0 rounded-[6px]" />
                        <div className="min-w-0">
                            <SlackName name="Warmbly" app time="9:43 AM" />
                            <div className="text-[12px] text-[#1d1c1d]">A draft from marcus.reid@sunriselabs.io:</div>
                            <p className="mt-1 border-l-[3px] border-[#dddddd] pl-2 text-[11.5px] leading-[1.5] text-[#1d1c1d]">
                                {words.map((w, i) => (
                                    <motion.span
                                        key={i}
                                        initial={reduced ? false : { opacity: 0 }}
                                        animate={{ opacity: 1 }}
                                        transition={{ duration: 0.12, delay: 1.8 + i * 0.06 }}
                                    >
                                        {w}{" "}
                                    </motion.span>
                                ))}
                            </p>
                            <Reveal delay={3.4} className="mt-2">
                                <SlackButton primary>Review and send</SlackButton>
                            </Reveal>
                        </div>
                    </Reveal>
                </div>
            </div>
        </SlackShell>
    );
}

const SCENES: Partial<Record<SceneId, ComponentType>> = {
    welcome: WelcomeScene,
    addAccount: AddAccountScene,
    accounts: AccountsScene,
    warmup: WarmupScene,
    domains: DomainsScene,
    campaignList: CampaignListScene,
    campaignSteps: CampaignStepsScene,
    campaignLive: CampaignLiveScene,
    contacts: ContactsScene,
    segments: SegmentsScene,
    inbox: InboxScene,
    inboxDrafts: InboxDraftsScene,
    analytics: AnalyticsScene,
    deliverability: DeliverabilityScene,
    deals: DealsScene,
    automationList: AutomationListScene,
    automationBuilder: AutomationBuilderScene,
    remieAsk: RemieAskScene,
    remieAct: RemieActScene,
    slackAsk: SlackAskScene,
    slackInbox: SlackInboxScene,
};

/** The scene a slide shows; nothing for a slide without one. */
export default function TourScene({ id }: { id: SceneId }) {
    const Scene = SCENES[id];
    return Scene ? <Scene /> : null;
}
