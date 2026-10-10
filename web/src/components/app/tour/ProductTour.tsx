// The product tour: dims the dashboard, spotlights each part where it really
// lives, and walks it chapter by chapter with sample data. Any chapter can be
// jumped to from the rail, the whole thing skipped (the button, or Escape),
// and Remie narrates every slide with the same job handed to it instead.

import { useCallback, useEffect, useId, useLayoutEffect, useMemo, useRef, useState, type TouchEvent } from "react";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import { useNavigate } from "@tanstack/react-router";
import toast from "react-hot-toast/headless";
import { ArrowLeftIcon, ArrowRightIcon, CheckIcon, XIcon } from "lucide-react";
import AgentMark from "@/components/app/agent/AgentMark";
import ScrollStrip from "@/components/ui/scroll-strip";
import { askRemie } from "@/components/app/agent/askRemie";
import useFeatureAccess from "@/hooks/useFeatureAccess";
import useUpgradeFlow from "@/hooks/useUpgradeFlow";
import useCompleteProductTour from "@/lib/api/hooks/auth/useCompleteProductTour";
import { usePermission } from "@/hooks/usePermission";
import { useUpgradeDialog } from "@/hooks/context/upgrade";
import { useUserProfile } from "@/hooks/context/user";
import { useIsMobile } from "@/hooks/use-mobile";
import { getPlan, type PlanID } from "@/lib/plans";
import type { BillingInterval } from "@/lib/pricing";
import { capture } from "@/lib/productAnalytics";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import TourStage from "./TourStage";
import TourScene from "./scenes";
import { EASE } from "./sceneTokens";
import { RemieCard, SlideCopy } from "./TourCopy";
import { CloudCallout, PlanChoose, PlanCompare, PlanIncluded } from "./PlanSlides";
import { TOUR_SLIDES, type FlatSlide } from "./tourSteps";
import { SHOTS } from "./tourShots";
import { CLOUD_DOCS, PRIMARY, SECONDARY, billingPreview, hostedPlanURL } from "./tourUi";

const CLOSE =
    "h-7 pl-2 pr-2.5 rounded-full inline-flex items-center gap-1 bg-white/85 text-[12px] text-slate-500 ring-1 ring-slate-900/[0.06] backdrop-blur-sm transition-colors hover:text-slate-900 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sky-400";

type Rect = { x: number; y: number; w: number; h: number };
type Outcome = "completed" | "skipped" | "ask_remie" | "open_remie" | "connect_slack" | "choose_plan" | "add_mailbox" | "link_cloud";

// The element a slide points at, when it is actually on screen: a sidebar row
// inside the closed mobile drawer, or in a folded section, has no box worth lighting.
function measure(anchor?: string): Rect | null {
    if (!anchor) return null;
    const el = document.querySelector<HTMLElement>(`[data-tour="${CSS.escape(anchor)}"]`);
    if (!el) return null;
    el.scrollIntoView({ block: "nearest" });
    const r = el.getBoundingClientRect();
    if (r.width < 4 || r.height < 4) return null;
    if (r.right <= 0 || r.bottom <= 0 || r.left >= window.innerWidth || r.top >= window.innerHeight) return null;
    return { x: r.left, y: r.top, w: r.width, h: r.height };
}

function navEdge(): number {
    const r = document.querySelector("[data-tour-nav]")?.getBoundingClientRect();
    return r && r.right > 0 ? r.right : 0;
}


export default function ProductTour() {
    const reduced = useReducedMotion();
    const isMobile = useIsMobile();
    const navigate = useNavigate();
    const setOpen = useAppStore((s) => s.setProductTourOpen);
    const firstName = useAppStore((s) => s.user?.first_name ?? "");
    const real = useFeatureAccess();
    const preview = useMemo(billingPreview, []);
    const access =
        preview === "free"
            ? { ...real, billing: true, paid: false, warmupOnly: false, locked: true, isOwner: true }
            : preview === "paid"
              ? { ...real, billing: true, paid: true, warmupOnly: false, locked: false, isOwner: true, plan: "grow" as const }
              : real;
    const upgrade = useUpgradeDialog();
    const flow = useUpgradeFlow();
    const markDone = useCompleteProductTour().mutate;
    const profile = useUserProfile();
    const canAI = usePermission("USE_AI");
    const canSettings = usePermission("MANAGE_SETTINGS");
    const canMailboxes = usePermission("MANAGE_EMAILS");

    const productPaid = access.paid && !access.warmupOnly;
    // A prompt Remie cannot act on would be a control that does nothing.
    const canAsk = canAI && productPaid;

    const [index, setIndex] = useState(0);
    const [dir, setDir] = useState(1);
    const [billingInterval, setBillingInterval] = useState<BillingInterval>("annual");
    // Set once a skip has been turned into a last look at the plans.
    const [jumped, setJumped] = useState(false);
    const slides = TOUR_SLIDES;
    // An instance without billing has every feature on; its plans are the hosted ones on warmbly.com.
    const selfHost = !access.billing;
    const slide = slides[Math.min(index, slides.length - 1)];
    const last = index >= slides.length - 1;

    const [rect, setRect] = useState<Rect | null>(null);
    const [left, setLeft] = useState(0);
    const [view, setView] = useState({ w: window.innerWidth, h: window.innerHeight });
    const center = useRef({ x: window.innerWidth / 2, y: window.innerHeight / 2 });

    useLayoutEffect(() => {
        const update = () => {
            const r = measure(slide.anchor);
            if (r) center.current = { x: r.x + r.w / 2, y: r.y + r.h / 2 };
            setRect(r);
            setLeft(isMobile ? 0 : navEdge());
            setView((v) => (v.w === window.innerWidth && v.h === window.innerHeight ? v : { w: window.innerWidth, h: window.innerHeight }));
        };
        update();
        // The sidebar can still be settling (a fold, the rail width) on the first frame.
        const raf = requestAnimationFrame(update);
        const late = window.setTimeout(update, 260);
        window.addEventListener("resize", update);
        return () => {
            cancelAnimationFrame(raf);
            window.clearTimeout(late);
            window.removeEventListener("resize", update);
        };
    }, [slide.anchor, isMobile]);

    // The overlay fades out after closing, and a key pressed meanwhile must not end it twice.
    const done = useRef(false);
    const finish = useCallback(
        (outcome: Outcome) => {
            if (done.current) return;
            done.current = true;
            markDone();
            setOpen(false);
            capture("product_tour_finished", { outcome, step: `${slide.chapter.id}.${slide.id}`, index, skipped_to_plans: jumped });
            if (outcome === "skipped") toast("You can replay the tour any time from your profile menu.");
        },
        [setOpen, slide, index, jumped, markDone],
    );

    const go = useCallback(
        (to: number) => {
            if (to < 0 || to >= slides.length || to === index) return;
            setDir(to > index ? 1 : -1);
            setIndex(to);
        },
        [index, slides.length],
    );

    // Skipping early still ends on the plans, once.
    const skip = useCallback(() => {
        if (!jumped && slide.chapter.id !== "plans") {
            setJumped(true);
            go(slides.length - 1);
            return;
        }
        finish(last && !jumped ? "completed" : "skipped");
    }, [jumped, slide.chapter.id, go, slides.length, finish, last]);

    // Fetch every screenshot up front, so no slide opens on a blank window.
    useEffect(() => {
        for (const src of SHOTS) new Image().src = src;
    }, []);

    const cardRef = useRef<HTMLDivElement>(null);
    const primaryRef = useRef<HTMLButtonElement>(null);
    const titleId = useId();

    // Focus follows the slide, and goes back where it was when the tour ends.
    useEffect(() => {
        const before = document.activeElement as HTMLElement | null;
        // Unless the tour's last action opened something that took focus (a dialog, Remie).
        return () => {
            const now = document.activeElement;
            if (!now || now === document.body) before?.focus?.();
        };
    }, []);
    useEffect(() => {
        primaryRef.current?.focus({ preventScroll: true });
    }, [index]);

    // Captured ahead of the app's shortcuts, which must not fire under the tour.
    useEffect(() => {
        const onKey = (e: KeyboardEvent) => {
            if (done.current) return;
            e.stopImmediatePropagation();
            if (e.key === "Escape") {
                e.preventDefault();
                skip();
            } else if (e.key === "ArrowRight" || e.key === "ArrowDown") {
                e.preventDefault();
                if (!last) go(index + 1);
            } else if (e.key === "ArrowLeft" || e.key === "ArrowUp") {
                e.preventDefault();
                go(index - 1);
            } else if (e.key === "Tab" && cardRef.current) {
                const items = [...cardRef.current.querySelectorAll<HTMLElement>('button:not([disabled]):not([tabindex="-1"]), [href]')];
                if (items.length === 0) return;
                const first = items[0];
                const end = items[items.length - 1];
                const at = document.activeElement;
                if (e.shiftKey && (at === first || !cardRef.current.contains(at))) {
                    e.preventDefault();
                    end.focus();
                } else if (!e.shiftKey && (at === end || !cardRef.current.contains(at))) {
                    e.preventDefault();
                    first.focus();
                }
            }
        };
        window.addEventListener("keydown", onKey, true);
        return () => window.removeEventListener("keydown", onKey, true);
    }, [skip, go, index, last]);

    const hole = rect
        ? { left: rect.x - 4, top: rect.y - 4, width: rect.w + 8, height: rect.h + 8 }
        : { left: center.current.x, top: center.current.y, width: 0, height: 0 };
    const spring = reduced ? { duration: 0 } : { type: "spring" as const, stiffness: 260, damping: 32 };
    const slideBy = reduced ? 0 : 28;

    // The layout follows the room the tour really has: a phone, a tablet, a short laptop, a landscape phone.
    const avail = view.w - left;
    const stack = isMobile || avail < 760 || view.h < 600;
    const showRail = !stack && avail >= 1080;

    const where = !rect || !slide.label ? null : slide.anchor === "remie" ? "In the top bar" : isMobile ? "In the menu" : "In the sidebar";

    // Only the owner changes the plan; the cards carry the choice, so the footer only defers it.
    const canChoose = !selfHost && access.isOwner;
    const planName = selfHost ? null : access.warmupOnly ? "Warmup" : productPaid ? getPlan(access.plan).label : null;
    const heading = selfHost
        ? {
              lead: jumped ? "Before you go." : "Hosted plans.",
              title: "Rather we ran it for you?",
              body: "Every feature in this tour is already on here, on your own server. A plan on warmbly.com is the same product, hosted, updated and backed up for you.",
          }
        : !planName
        ? { lead: jumped ? "Before you go." : undefined }
        : {
              lead: jumped ? "Before you go." : "Your plan.",
              title: `You're on ${planName}.`,
              body: productPaid
                  ? "Everything in this tour is already on for your workspace. Need more sending or AI credits? Change plan any time, and the difference is prorated."
                  : "Your mailboxes warm in the premium pool. Choose a plan to start sending and to turn on the inbox, the CRM, automations and Remie.",
          };
    const choosePlan = async (id: PlanID) => {
        if (preview) {
            toast(`Preview: this would open checkout for ${id}.`);
            return;
        }
        if (id === "enterprise") {
            act("choose_plan", () => upgrade.open({ feature: "Enterprise", minPlan: "enterprise" }));
            return;
        }
        const outcome = await flow.upgrade(id, { interval: billingInterval, returnTo: window.location.pathname + window.location.search });
        if (outcome === "redirect" || outcome === "changed" || outcome === "portal") finish("choose_plan");
        else if (outcome === "contact") act("choose_plan", () => upgrade.open({ feature: "Enterprise", minPlan: "enterprise" }));
    };
    const mailboxAction = canMailboxes
        ? { label: "Connect a mailbox", outcome: "add_mailbox" as Outcome, run: () => profile.setAddEmail(true) }
        : null;

    const act = (outcome: Outcome, run: () => void) => {
        finish(outcome);
        run();
    };

    const next = slides[index + 1];
    let primary: { label: string; onClick: () => void; icon?: boolean; quiet?: boolean };
    if (!last) {
        const label = index === 0 ? "Show me around" : next.si === 0 && !stack ? `Next: ${next.chapter.name}` : "Next";
        primary = { label, onClick: () => go(index + 1), icon: true };
    } else if (canChoose && !productPaid) {
        primary = { label: "Maybe later", onClick: () => finish(jumped ? "skipped" : "completed"), quiet: true };
    } else if (mailboxAction) {
        primary = { label: mailboxAction.label, onClick: () => act(mailboxAction.outcome, mailboxAction.run) };
    } else {
        primary = { label: "Finish", onClick: () => finish("completed") };
    }
    const secondary: { label: string; onClick: () => void } | null = !last
        ? null
        : canChoose && !productPaid && mailboxAction
          ? { label: "Connect a mailbox first", onClick: () => act(mailboxAction.outcome, mailboxAction.run) }
          : primary.label !== "Finish"
            ? { label: "Done", onClick: () => finish("completed") }
            : null;

    const openRemie = canAsk ? () => act("open_remie", () => useAppStore.getState().setAIAssistantOpen(true)) : undefined;
    const connectSlack =
        canSettings && !access.locked
            ? () => act("connect_slack", () => navigate({ to: "/app/integrations/$", params: { _splat: "slack" } }))
            : undefined;

    const onAsk = (prompt: string) => act("ask_remie", () => askRemie(prompt));
    const current = !selfHost && access.paid ? (access.warmupOnly ? "warmup" : access.plan) : null;
    const panel = { slide, titleId };
    const remieCard = slide.remie || slide.ask ? <RemieCard slide={slide} canAsk={canAsk} onAsk={onAsk} /> : undefined;

    // A horizontal swipe on a touch screen moves between slides, as the arrow keys do.
    const touch = useRef<{ x: number; y: number } | null>(null);
    const onTouchStart = (e: TouchEvent) => {
        const t = e.touches[0];
        touch.current = e.touches.length === 1 ? { x: t.clientX, y: t.clientY } : null;
    };
    const onTouchEnd = (e: TouchEvent) => {
        const from = touch.current;
        touch.current = null;
        if (!from || (e.target as HTMLElement).closest("[data-no-swipe]")) return;
        const t = e.changedTouches[0];
        const dx = t.clientX - from.x;
        const dy = t.clientY - from.y;
        if (Math.abs(dx) < 56 || Math.abs(dx) < Math.abs(dy) * 1.5) return;
        if (dx < 0 && !last) go(index + 1);
        else if (dx > 0) go(index - 1);
    };

    return (
        <motion.div
            className="fixed inset-0 z-[160]"
            initial={reduced ? false : { opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: reduced ? 0 : 0.25 }}
        >
            {/* The scrim is the spotlight's own shadow, so the lit element shows through it. */}
            <motion.div
                aria-hidden
                className="pointer-events-none absolute rounded-[10px] shadow-[0_0_0_200vmax_rgba(15,23,42,0.5)]"
                initial={false}
                animate={hole}
                transition={spring}
            >
                <motion.span className="absolute inset-0 rounded-[10px] ring-2 ring-white" initial={false} animate={{ opacity: rect ? 1 : 0 }} />
                {rect && !reduced && (
                    <motion.span
                        key={slide.anchor}
                        className="absolute -inset-1 rounded-[12px] ring-2 ring-sky-400"
                        initial={{ opacity: 0.8, scale: 1 }}
                        animate={{ opacity: 0, scale: 1.12 }}
                        transition={{ duration: 1.6, repeat: Infinity, ease: "easeOut", delay: 0.4 }}
                    />
                )}
            </motion.div>

            <div
                className={cn("absolute inset-y-0 right-0 flex", stack ? "items-end p-2" : "items-center justify-center pt-[64px] pb-5 px-5")}
                style={{ left }}
            >
                <motion.div
                    ref={cardRef}
                    role="dialog"
                    aria-modal="true"
                    aria-labelledby={titleId}
                    className={cn(
                        "relative flex w-full overflow-hidden bg-white ring-1 ring-slate-900/[0.08] shadow-[0_50px_100px_-40px_rgba(15,23,42,0.55)]",
                        stack
                            ? "h-[calc(100dvh-16px)] max-h-[820px] rounded-[14px] pb-[env(safe-area-inset-bottom)]"
                            : "max-w-[1320px] h-[min(860px,calc(100dvh-84px))] rounded-[16px]",
                    )}
                    initial={reduced ? false : { opacity: 0, y: 14, scale: 0.985 }}
                    animate={{ opacity: 1, y: 0, scale: 1 }}
                    transition={{ duration: 0.45, ease: EASE }}
                >
                    {showRail && <Rail slides={slides} index={index} onPick={go} onSkip={skip} last={last} />}

                    <div className="@container relative flex min-w-0 flex-1 flex-col">
                        {showRail ? (
                            <button type="button" onClick={skip} className={cn(CLOSE, "absolute right-3 top-3 z-20")}>
                                <XIcon className="size-3.5" /> {last ? "Close" : "Skip tour"}
                            </button>
                        ) : (
                            <div className="flex h-12 shrink-0 items-center gap-2 border-b border-slate-100 pl-2 pr-2.5">
                                <ChapterStrip slides={slides} index={index} onPick={go} />
                                <button type="button" onClick={skip} className={cn(CLOSE, "shrink-0")}>
                                    <XIcon className="size-3.5" /> {last ? "Close" : "Skip"}
                                </button>
                            </div>
                        )}

                        <div className="relative min-h-0 flex-1 overflow-hidden" onTouchStart={onTouchStart} onTouchEnd={onTouchEnd}>
                            <AnimatePresence initial={false}>
                                <motion.div
                                    key={index}
                                    className={cn("absolute inset-0 flex flex-col", stack && "overflow-y-auto overscroll-contain")}
                                    initial={{ opacity: 0, x: dir * slideBy }}
                                    animate={{ opacity: 1, x: 0 }}
                                    exit={{ opacity: 0, x: -dir * slideBy }}
                                    transition={{ duration: reduced ? 0 : 0.38, ease: EASE }}
                                >
                                    {slide.scene === "planIncluded" ? (
                                        <PlanIncluded {...panel} remie={remieCard} />
                                    ) : slide.scene === "planCompare" ? (
                                        <PlanCompare {...panel} remie={remieCard} interval={billingInterval} onInterval={setBillingInterval} current={current} />
                                    ) : slide.scene === "plans" ? (
                                        selfHost ? (
                                            <PlanChoose
                                                {...panel}
                                                {...heading}
                                                interval={billingInterval}
                                                onInterval={setBillingInterval}
                                                current={null}
                                                pending={null}
                                                ownerOnly={false}
                                                hrefFor={hostedPlanURL}
                                                onOpen={(id) => capture("product_tour_hosted_plan", { plan: id })}
                                            >
                                                <CloudCallout
                                                    docs={CLOUD_DOCS}
                                                    onLink={canSettings ? () => act("link_cloud", () => navigate({ to: "/app/settings/warmbly-cloud" })) : undefined}
                                                />
                                            </PlanChoose>
                                        ) : (
                                            <PlanChoose
                                                {...panel}
                                                {...heading}
                                                interval={billingInterval}
                                                onInterval={setBillingInterval}
                                                current={current}
                                                onChoose={canChoose ? (id) => void choosePlan(id) : undefined}
                                                pending={flow.pending}
                                                ownerOnly={!access.isOwner}
                                                note={
                                                    access.paid
                                                        ? undefined
                                                        : "Only warming for now? Free keeps up to 10 mailboxes warming, no card needed, and the Warmup plan puts every mailbox in the premium pool for $15 a month."
                                                }
                                            />
                                        )
                                    ) : (
                                        <>
                                            <div className={cn(stack ? "h-[clamp(170px,36dvh,300px)] shrink-0" : "min-h-0 flex-1 p-3 pb-0")}>
                                                <TourStage className={cn(!stack && "rounded-[10px]")}>
                                                    <TourScene id={slide.scene} />
                                                </TourStage>
                                            </div>
                                            <SlideCopy
                                                slide={slide}
                                                titleId={titleId}
                                                where={where}
                                                firstName={firstName}
                                                mobile={stack}
                                                canAsk={canAsk}
                                                onAsk={onAsk}
                                                onOpenRemie={slide.scene === "remieAct" ? openRemie : undefined}
                                                onConnectSlack={slide.slack ? connectSlack : undefined}
                                            />
                                        </>
                                    )}
                                </motion.div>
                            </AnimatePresence>
                        </div>

                        <div className="shrink-0 border-t border-slate-100 px-3 @md:px-6 h-16 flex items-center gap-2 @md:gap-3">
                            <Progress slides={slides} index={index} mobile={stack} />
                            <div className="ml-auto flex items-center gap-2">
                                {index > 0 && (
                                    <button type="button" onClick={() => go(index - 1)} className={cn(SECONDARY, "w-10 px-0")} aria-label="Back">
                                        <ArrowLeftIcon className="size-4" />
                                    </button>
                                )}
                                {secondary && (
                                    <button type="button" onClick={secondary.onClick} className={cn(SECONDARY, "hidden @2xl:inline-flex")}>
                                        {secondary.label}
                                    </button>
                                )}
                                <button ref={primaryRef} type="button" onClick={primary.onClick} className={primary.quiet ? SECONDARY : PRIMARY}>
                                    {primary.label}
                                    {primary.icon && <ArrowRightIcon className="size-3.5" />}
                                </button>
                            </div>
                        </div>
                    </div>
                </motion.div>
            </div>
        </motion.div>
    );
}

// Every chapter, clickable: done ones ticked, the current one with a mark per slide.
function Rail({
    slides,
    index,
    onPick,
    onSkip,
    last,
}: {
    slides: FlatSlide[];
    index: number;
    onPick: (i: number) => void;
    onSkip: () => void;
    last: boolean;
}) {
    const at = slides[index];
    const starts = slides.flatMap((s, i) => (s.si === 0 ? [i] : []));
    const chapters = starts.map((i) => slides[i].chapter);
    return (
        <nav aria-label="Tour chapters" className="flex w-[216px] shrink-0 flex-col border-r border-slate-100 bg-slate-50/60 px-3 pt-5 pb-4">
            <div className="flex items-center gap-2 px-2">
                <AgentMark size={18} />
                <span className="text-[13px] font-medium text-slate-900">Product tour</span>
            </div>
            <div className="mt-1 px-2 text-[11.5px] text-slate-400">
                {chapters.length} chapters, with Remie along
            </div>

            <ol className="mt-5 flex-1 space-y-0.5 overflow-y-auto">
                {chapters.map((chapter, ci) => {
                    const current = ci === at.ci;
                    const passed = ci < at.ci;
                    return (
                        <li key={chapter.id}>
                            <button
                                type="button"
                                onClick={() => onPick(starts[ci])}
                                aria-current={current ? "step" : undefined}
                                className={cn(
                                    "group w-full rounded-[8px] px-2 py-1.5 text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sky-400",
                                    current ? "bg-white ring-1 ring-slate-200 shadow-[0_1px_2px_rgba(15,23,42,0.04)]" : "hover:bg-white/70",
                                )}
                            >
                                <span className="flex items-center gap-2.5">
                                    <span
                                        className={cn(
                                            "size-5 shrink-0 rounded-full inline-flex items-center justify-center text-[10px] font-medium tabular-nums transition-colors",
                                            passed ? "bg-slate-900 text-white" : current ? "bg-sky-600 text-white" : "bg-white text-slate-400 ring-1 ring-slate-200",
                                        )}
                                    >
                                        {passed ? <CheckIcon className="size-3" strokeWidth={3} /> : ci + 1}
                                    </span>
                                    <span className={cn("text-[13px]", current ? "font-medium text-slate-900" : passed ? "text-slate-600" : "text-slate-500")}>
                                        {chapter.name}
                                    </span>
                                </span>
                                {current && chapter.slides.length > 1 && (
                                    <span className="mt-1.5 ml-[30px] flex gap-1">
                                        {chapter.slides.map((s, si) => (
                                            <span
                                                key={s.id}
                                                className={cn("h-1 flex-1 rounded-full transition-colors duration-300", si <= at.si ? "bg-sky-500" : "bg-slate-200")}
                                            />
                                        ))}
                                    </span>
                                )}
                            </button>
                        </li>
                    );
                })}
            </ol>

            <div className="mt-3 border-t border-slate-200/70 px-2 pt-3">
                {!last && (
                    <button
                        type="button"
                        onClick={onSkip}
                        className="text-[12.5px] text-slate-500 underline-offset-2 hover:text-slate-900 hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sky-400 rounded"
                    >
                        Skip the tour
                    </button>
                )}
                <div className="mt-1 text-[11px] text-slate-400">← → to move, Esc to close</div>
            </div>
        </nav>
    );
}

// The chapter list where the side rail does not fit: one scrolling row of chips.
function ChapterStrip({ slides, index, onPick }: { slides: FlatSlide[]; index: number; onPick: (i: number) => void }) {
    const at = slides[index];
    const starts = slides.flatMap((s, i) => (s.si === 0 ? [i] : []));
    return (
        <nav aria-label="Tour chapters" className="min-w-0 flex-1" data-no-swipe>
            <ScrollStrip activeKey={at.chapter.id} innerClassName="flex items-center gap-1 py-1">
                {starts.map((start, ci) => {
                    const chapter = slides[start].chapter;
                    const current = ci === at.ci;
                    return (
                        <button
                            key={chapter.id}
                            type="button"
                            data-active={current ? "true" : undefined}
                            aria-current={current ? "step" : undefined}
                            onClick={() => onPick(start)}
                            className={cn(
                                "h-8 shrink-0 rounded-full px-3 inline-flex items-center gap-1.5 text-[12.5px] transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sky-400",
                                current ? "bg-slate-900 text-white" : ci < at.ci ? "text-slate-700 hover:bg-slate-100" : "text-slate-400 hover:bg-slate-100 hover:text-slate-700",
                            )}
                        >
                            {ci < at.ci && <CheckIcon className="size-3" strokeWidth={3} />}
                            {chapter.name}
                        </button>
                    );
                })}
            </ScrollStrip>
        </nav>
    );
}

function Progress({ slides, index, mobile }: { slides: FlatSlide[]; index: number; mobile: boolean }) {
    const at = slides[index];
    const share = ((index + 1) / slides.length) * 100;
    return (
        <div className="flex min-w-0 items-center gap-3">
            <div className="h-1 w-24 sm:w-40 shrink-0 overflow-hidden rounded-full bg-slate-100">
                <motion.div className="h-full rounded-full bg-slate-900" initial={false} animate={{ width: `${share}%` }} transition={{ duration: 0.4, ease: EASE }} />
            </div>
            <span className="truncate text-[12px] tabular-nums text-slate-400">
                {mobile ? `${index + 1} / ${slides.length}` : `${at.chapter.name} · ${index + 1} of ${slides.length}`}
            </span>
        </div>
    );
}
