import React from "react";
import { motion, useReducedMotion } from "motion/react";
import { Flame, Inbox, BarChart3, Send, CheckCircle2 } from "lucide-react";

/* ═══════════════════════════════════════════
   Auth showcase — a horizontal slide carousel on an airy sky.

   Slides translate left like a reel. A cloned first slide at the end lets
   it loop forward seamlessly (animate to the clone, then snap back with no
   transition), so it never rewinds and never stalls on the last slide.
   Sky = CSS gradient + WebP clouds (no SVG filters).
   ═══════════════════════════════════════════ */

const SLIDE_MS = 4600;

interface Slide {
    key: string;
    icon: React.ComponentType<{ className?: string }>;
    title: string;
    desc: string;
    mock: React.ReactNode;
}

function Panel({ children }: { children: React.ReactNode }) {
    return (
        <div className="w-full rounded-2xl border border-white/60 bg-white/95 px-5 py-4 shadow-[0_30px_60px_-30px_rgba(2,8,23,0.55)] backdrop-blur-sm">
            {children}
        </div>
    );
}

const Eyebrow = ({ children }: { children: React.ReactNode }) => (
    <div className="text-[10px] font-semibold uppercase tracking-[0.12em] text-slate-400">{children}</div>
);

function Spark() {
    return (
        <svg viewBox="0 0 120 30" className="w-full h-8" fill="none" preserveAspectRatio="none">
            <defs>
                <linearGradient id="spk" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor="rgba(14,165,233,0.20)" />
                    <stop offset="100%" stopColor="rgba(14,165,233,0)" />
                </linearGradient>
            </defs>
            <path d="M0 25 L24 22 L48 23 L72 15 L96 9 L120 3 L120 30 L0 30 Z" fill="url(#spk)" />
            <path d="M0 25 L24 22 L48 23 L72 15 L96 9 L120 3" stroke="#0ea5e9" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
    );
}

function Ring({ pct }: { pct: number }) {
    const r = 24;
    const c = 2 * Math.PI * r;
    return (
        <div className="relative size-[58px] shrink-0">
            <svg viewBox="0 0 60 60" className="size-[58px] -rotate-90">
                <circle cx="30" cy="30" r={r} stroke="#e2e8f0" strokeWidth="5" fill="none" />
                <circle cx="30" cy="30" r={r} stroke="#0ea5e9" strokeWidth="5" fill="none" strokeLinecap="round" strokeDasharray={c} strokeDashoffset={c * (1 - pct / 100)} />
            </svg>
            <span className="absolute inset-0 flex items-center justify-center text-[13px] font-bold text-slate-800 tabular-nums">{pct}</span>
        </div>
    );
}

const slides: Slide[] = [
    {
        key: "warmup",
        icon: Flame,
        title: "Warmup on autopilot",
        desc: "Reputation built with natural, well-spaced traffic.",
        mock: (
            <Panel>
                <div className="flex items-center justify-between">
                    <Eyebrow>Reputation</Eyebrow>
                    <span className="inline-flex items-center gap-1 text-[10.5px] font-medium text-emerald-600">
                        <CheckCircle2 className="size-3" /> Healthy
                    </span>
                </div>
                <div className="mt-1 text-[34px] font-bold leading-none text-slate-900 tabular-nums">
                    98<span className="text-[15px] text-slate-300"> / 100</span>
                </div>
                <div className="mt-3"><Spark /></div>
            </Panel>
        ),
    },
    {
        key: "inbox",
        icon: Inbox,
        title: "Land in the primary tab",
        desc: "Live inbox placement across every mailbox.",
        mock: (
            <Panel>
                <div className="flex items-center gap-4">
                    <Ring pct={96} />
                    <div>
                        <Eyebrow>Inbox placement</Eyebrow>
                        <div className="mt-0.5 text-[20px] font-bold leading-tight text-slate-900">Primary</div>
                        <div className="text-[12px] text-slate-400">96% inboxed this week</div>
                    </div>
                </div>
            </Panel>
        ),
    },
    {
        key: "analytics",
        icon: BarChart3,
        title: "Know what's working",
        desc: "Opens, replies and placement, beautifully tracked.",
        mock: (
            <Panel>
                <div className="flex items-center justify-between gap-4">
                    <div>
                        <Eyebrow>Open rate</Eyebrow>
                        <div className="mt-0.5 text-[28px] font-bold leading-none text-slate-900 tabular-nums">62%</div>
                    </div>
                    <div>
                        <Eyebrow>Reply rate</Eyebrow>
                        <div className="mt-0.5 text-[28px] font-bold leading-none text-slate-900 tabular-nums">14%</div>
                    </div>
                </div>
                <div className="mt-3"><Spark /></div>
            </Panel>
        ),
    },
    {
        key: "sequences",
        icon: Send,
        title: "Automated sequences",
        desc: "Multi-step outreach that respects every reply.",
        mock: (
            <Panel>
                <div className="space-y-2.5">
                    {[["Intro email", "Sent"], ["Follow-up", "Scheduled"], ["Break-up", "Queued"]].map(([s, st], idx) => (
                        <div key={s} className="flex items-center gap-3">
                            <span className={`size-5 shrink-0 rounded-full text-[10px] font-semibold flex items-center justify-center ${idx === 0 ? "bg-sky-500 text-white" : "bg-slate-100 text-slate-400"}`}>{idx + 1}</span>
                            <span className="text-[12.5px] font-medium text-slate-700">{s}</span>
                            <span className="ml-auto text-[11px] text-slate-400">{st}</span>
                        </div>
                    ))}
                </div>
            </Panel>
        ),
    },
];

const N = slides.length;
const track = [...slides, slides[0]]; // clone first slide for a seamless forward loop

function SlideView({ slide }: { slide: Slide }) {
    const Icon = slide.icon;
    return (
        <div className="flex h-full w-full shrink-0 flex-col px-7 pb-9 pt-9 xl:px-8">
            <div className="flex flex-1 items-center">
                {/* Full-width card so a feature is always on screen as it slides in */}
                <div className="w-full">{slide.mock}</div>
            </div>
            <div className="pt-6">
                <div className="mb-1 flex items-center gap-2">
                    <Icon className="size-4 text-sky-200" />
                    <h3 className="text-[15px] font-bold tracking-tight text-white">{slide.title}</h3>
                </div>
                <p className="text-[12.5px] leading-relaxed text-white/65 max-w-[280px]">{slide.desc}</p>
            </div>
        </div>
    );
}

export default function AuthShowcase() {
    const reduce = useReducedMotion();
    const [idx, setIdx] = React.useState(0); // 0..N, where N renders the clone
    const [withAnim, setWithAnim] = React.useState(true);
    const idxRef = React.useRef(idx);
    idxRef.current = idx;

    // Auto-advance. Paused while the tab is hidden — otherwise the interval
    // keeps firing while animations/rAF are frozen, the index runs past the
    // track, and the pane shows blank space when you switch back.
    React.useEffect(() => {
        let id: ReturnType<typeof setInterval> | undefined;
        const start = () => {
            if (id === undefined) id = setInterval(() => setIdx((p) => p + 1), SLIDE_MS);
        };
        const stop = () => {
            if (id !== undefined) {
                clearInterval(id);
                id = undefined;
            }
        };
        const onVisibility = () => {
            if (document.hidden) {
                stop();
                return;
            }
            // Back on the tab: if we were parked on the clone, snap to the real
            // first slide instantly so nothing is left off-screen, then resume.
            if (idxRef.current >= N) {
                setWithAnim(false);
                setIdx(0);
            }
            start();
        };
        if (!document.hidden) start();
        document.addEventListener("visibilitychange", onVisibility);
        return () => {
            stop();
            document.removeEventListener("visibilitychange", onVisibility);
        };
    }, []);

    // After the snap (no-transition reset), re-enable animation next frame.
    React.useEffect(() => {
        if (withAnim) return;
        const r = requestAnimationFrame(() => setWithAnim(true));
        return () => cancelAnimationFrame(r);
    }, [withAnim]);

    const real = idx % N;

    const goTo = (k: number) => {
        setWithAnim(true);
        setIdx(k);
    };

    return (
        <div className="relative flex h-full flex-col overflow-hidden">
            {/* Airy sky — same palette as the marketing-site hero */}
            <div className="sky-base" aria-hidden="true" />
            <div className="sky-breathe" aria-hidden="true" />
            <div className="sun-glow" aria-hidden="true" />
            <img src="/backdrops/cloud-3.webp" alt="" aria-hidden="true" decoding="async" className="cloud-drift cloud-1 absolute select-none" style={{ top: "-6%", left: "-14%", width: 260, opacity: 0.6, height: "auto" }} />
            <img src="/backdrops/cloud-4.webp" alt="" aria-hidden="true" decoding="async" className="cloud-drift cloud-2 absolute select-none" style={{ bottom: "12%", right: "-12%", width: 220, opacity: 0.45, height: "auto" }} />

            {/* Carousel viewport */}
            <div className="relative z-10 flex-1 overflow-hidden">
                <motion.div
                    className="flex h-full"
                    animate={{ x: `-${idx * 100}%` }}
                    transition={withAnim && !reduce ? { duration: 0.6, ease: [0.16, 1, 0.3, 1] } : { duration: 0 }}
                    onAnimationComplete={() => {
                        if (idx === N) {
                            setWithAnim(false);
                            setIdx(0);
                        }
                    }}
                >
                    {track.map((s, k) => (
                        <SlideView key={k} slide={s} />
                    ))}
                </motion.div>
            </div>

            {/* Progress bars */}
            <div className="relative z-10 flex items-center gap-1.5 px-9 pb-9 xl:px-10">
                {slides.map((s, k) => (
                    <button
                        key={s.key}
                        type="button"
                        onClick={() => goTo(k)}
                        aria-label={`Show ${s.title}`}
                        className="h-1 flex-1 overflow-hidden rounded-full bg-white/25 cursor-pointer"
                    >
                        <motion.span
                            className="block h-full rounded-full bg-white"
                            initial={false}
                            animate={{ width: k === real ? "100%" : "0%" }}
                            transition={{ duration: 0.4, ease: "easeOut" }}
                        />
                    </button>
                ))}
            </div>
        </div>
    );
}
