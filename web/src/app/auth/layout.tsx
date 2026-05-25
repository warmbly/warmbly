import React from "react";
import { Navigate, Outlet, useNavigate } from "react-router-dom";
import { APP_URL, WEBSITE_URL } from "@/lib/information";
import getToken from "@/lib/helper/getToken";
import { Logo } from "@/components/svg";
import { Mail, BarChart3, Zap, Shield, Github, ArrowRight } from "lucide-react";

/* ═══════════════════════════════════════════
   Realistic Cloud — svg-clouds layered technique
   Each cloud = 3 stacked divs with DIFFERENT sizes, colors, positions.
   No mix-blend-mode — direct white/gray backgrounds distorted by SVG filters.

   LAYER STRUCTURE (svg-clouds repo):
   Back:  100% size, white         → main cloud body, bright fluffy mass
   Mid:   ~88% w, ~35% h at bottom → light gray for depth transition
   Front: ~76% w, ~15% h at bottom → darker gray underbelly shadow

   border-radius: 50% 50% 8% 8%   → round top, flat bottom (cumulus shape)
   box-shadow adds soft glow around the distorted shape edges.
   -webkit-filter prefix for Safari compatibility.
   ═══════════════════════════════════════════ */

function Cloud({
    className,
    style,
    filterSet,
    width,
    height,
    opacity = 1,
}: {
    className?: string;
    style?: React.CSSProperties;
    filterSet: "a" | "b" | "c";
    width: number;
    height: number;
    opacity?: number;
}) {
    const cloudRadius = "50% 50% 8% 8%";
    const backF = `url(#${filterSet}-back)`;
    const midF = `url(#${filterSet}-mid)`;
    const frontF = `url(#${filterSet}-front)`;

    return (
        <div
            className={`absolute pointer-events-none select-none ${className ?? ""}`}
            style={{ ...style, opacity }}
        >
            {/* Back layer — full cloud body, bright white, organic fluffy edges */}
            <div style={{
                position: "absolute",
                width,
                height,
                backgroundColor: "#fff",
                borderRadius: cloudRadius,
                boxShadow: "0 0 60px 20px rgba(255,255,255,0.8)",
                filter: backF,
                WebkitFilter: backF,
            }} />
            {/* Mid layer — lower portion, light blue-gray, depth transition */}
            <div style={{
                position: "absolute",
                width: Math.round(width * 0.88),
                height: Math.round(height * 0.35),
                left: Math.round(width * 0.06),
                top: Math.round(height * 0.62),
                backgroundColor: "#c4cfe0",
                borderRadius: cloudRadius,
                boxShadow: "0 0 45px 10px rgba(196,207,224,0.6)",
                filter: midF,
                WebkitFilter: midF,
            }} />
            {/* Front layer — bottom sliver, darker gray, underbelly shadow */}
            <div style={{
                position: "absolute",
                width: Math.round(width * 0.76),
                height: Math.round(height * 0.15),
                left: Math.round(width * 0.12),
                top: Math.round(height * 0.82),
                backgroundColor: "#8d9bb5",
                borderRadius: cloudRadius,
                boxShadow: "0 0 35px 8px rgba(141,155,181,0.5)",
                filter: frontF,
                WebkitFilter: frontF,
            }} />
        </div>
    );
}

/* ── Features ─────────────────────── */

const features = [
    { icon: Mail, label: "Smart Email Warmup", desc: "Gradually build sender reputation" },
    { icon: BarChart3, label: "Deliverability Analytics", desc: "Real-time inbox placement tracking" },
    { icon: Zap, label: "Campaign Automation", desc: "Launch outreach sequences effortlessly" },
    { icon: Shield, label: "Reputation Protection", desc: "Stay out of spam folders permanently" },
];

/* ═══════════════════════════════════════════
   Auth Layout — Living sky with layered clouds
   ═══════════════════════════════════════════ */

// `redirectIfAuthenticated` defaults to true so the bare /auth/* mount
// kicks already-signed-in users back into the app. OnboardingLayout
// reuses this component for its visual chrome but passes false — the
// onboarding page is *meant* to be reached while authenticated, and
// without this opt-out the AuthLayout guard bounces to /app/emails
// while UserProvider bounces back to /onboarding, producing an
// infinite history.replaceState loop.
export default function AuthLayout({
    redirectIfAuthenticated = true,
}: { redirectIfAuthenticated?: boolean } = {}) {
    const navigate = useNavigate();

    React.useEffect(() => {
        const receiveMessage = (event: MessageEvent) => {
            if (event.origin !== APP_URL) return;
            if (event.data?.type === "auth") navigate("/app/emails");
        };
        window.addEventListener("message", receiveMessage);
        return () => window.removeEventListener("message", receiveMessage);
    }, [navigate]);

    // Already signed in? Skip the auth UI entirely and send the user
    // into the app. /app's own guard will redirect onward to
    // /select-org or /onboarding if needed. Sits after hooks so the
    // Rules of Hooks aren't violated when the token state changes.
    if (redirectIfAuthenticated && getToken()) {
        return <Navigate to="/app/emails" replace />;
    }

    return (
        <div
            className="fixed inset-0"
            style={{
                overflow: "clip",
                background: "radial-gradient(ellipse 140% 140% at 72% 25%, #38bdf8 0%, #0ea5e9 18%, #0284c7 36%, #075985 58%, #0c4a6e 82%)",
            }}
        >

            {/* ── Sky breathe — brightness pulse with subtle scale ── */}
            <div
                className="absolute inset-0 pointer-events-none"
                style={{
                    background: "radial-gradient(ellipse 140% 140% at 72% 25%, #7dd3fc 0%, #38bdf8 18%, #0ea5e9 36%, #0284c7 58%, #075985 82%)",
                    animation: "sky-breathe 30s ease-in-out infinite",
                }}
            />

            {/* ── Ambient light drift — warm light slowly roaming ── */}
            <div
                className="absolute inset-0 pointer-events-none z-[1]"
                style={{
                    background: "radial-gradient(ellipse 50% 50% at 65% 30%, rgba(186,230,253,0.10) 0%, rgba(56,189,248,0.04) 40%, transparent 65%)",
                    animation: "light-drift 45s ease-in-out infinite",
                }}
            />

            {/* ── Horizon haze — atmospheric glow at the bottom edge ── */}
            <div
                className="absolute inset-x-0 bottom-0 h-[45%] pointer-events-none z-[1]"
                style={{
                    background: "linear-gradient(to top, rgba(125,211,252,0.12) 0%, rgba(56,189,248,0.06) 35%, transparent 100%)",
                }}
            />

            {/* ── Deep atmosphere scatter — breaks up flat dark zones ── */}
            <div
                className="absolute pointer-events-none z-[1]"
                style={{
                    width: "120%",
                    height: "120%",
                    left: "-20%",
                    top: "10%",
                    background: "radial-gradient(ellipse 60% 50% at 25% 70%, rgba(14,165,233,0.10) 0%, rgba(7,89,133,0.05) 40%, transparent 70%)",
                    animation: "atmo-drift 55s ease-in-out infinite",
                }}
            />

            {/* ── Upper atmosphere haze — thin veil across the dark side ── */}
            <div
                className="absolute pointer-events-none z-[1]"
                style={{
                    width: "100%",
                    height: "100%",
                    background: "radial-gradient(ellipse 80% 40% at 10% 50%, rgba(56,189,248,0.07) 0%, rgba(14,165,233,0.03) 50%, transparent 80%)",
                    animation: "atmo-drift-2 40s ease-in-out infinite",
                }}
            />

            {/* ── SVG cloud filters — 3 sets (a, b, c), each with back/mid/front ──
                 feTurbulence → feDisplacementMap → feGaussianBlur
                 Raw noise drives displacement (organic irregular shapes),
                 then blur softens the hard edges of the distorted result.
                 Back: heavy distortion + most blur → soft fluffy perimeter
                 Mid:  medium distortion + moderate blur → depth bridge
                 Front: light distortion + least blur → defined underbelly
            ── */}
            <svg className="absolute" width="0" height="0" aria-hidden="true">
                <defs>
                    {/* ── Set A ── */}
                    <filter id="a-back" x="-100%" y="-100%" width="350%" height="350%">
                        <feTurbulence type="fractalNoise" baseFrequency="0.012" numOctaves="4" seed="2" result="noise" />
                        <feDisplacementMap in="SourceGraphic" in2="noise" scale="180" result="displaced" />
                        <feGaussianBlur in="displaced" stdDeviation="4" />
                    </filter>
                    <filter id="a-mid" x="-100%" y="-100%" width="350%" height="350%">
                        <feTurbulence type="fractalNoise" baseFrequency="0.012" numOctaves="3" seed="3" result="noise" />
                        <feDisplacementMap in="SourceGraphic" in2="noise" scale="140" result="displaced" />
                        <feGaussianBlur in="displaced" stdDeviation="14" />
                    </filter>
                    <filter id="a-front" x="-100%" y="-100%" width="350%" height="350%">
                        <feTurbulence type="fractalNoise" baseFrequency="0.012" numOctaves="2" seed="4" result="noise" />
                        <feDisplacementMap in="SourceGraphic" in2="noise" scale="100" result="displaced" />
                        <feGaussianBlur in="displaced" stdDeviation="18" />
                    </filter>
                    {/* ── Set B ── */}
                    <filter id="b-back" x="-100%" y="-100%" width="350%" height="350%">
                        <feTurbulence type="fractalNoise" baseFrequency="0.012" numOctaves="4" seed="17" result="noise" />
                        <feDisplacementMap in="SourceGraphic" in2="noise" scale="180" result="displaced" />
                        <feGaussianBlur in="displaced" stdDeviation="4" />
                    </filter>
                    <filter id="b-mid" x="-100%" y="-100%" width="350%" height="350%">
                        <feTurbulence type="fractalNoise" baseFrequency="0.012" numOctaves="3" seed="18" result="noise" />
                        <feDisplacementMap in="SourceGraphic" in2="noise" scale="140" result="displaced" />
                        <feGaussianBlur in="displaced" stdDeviation="14" />
                    </filter>
                    <filter id="b-front" x="-100%" y="-100%" width="350%" height="350%">
                        <feTurbulence type="fractalNoise" baseFrequency="0.012" numOctaves="2" seed="19" result="noise" />
                        <feDisplacementMap in="SourceGraphic" in2="noise" scale="100" result="displaced" />
                        <feGaussianBlur in="displaced" stdDeviation="18" />
                    </filter>
                    {/* ── Set C ── */}
                    <filter id="c-back" x="-100%" y="-100%" width="350%" height="350%">
                        <feTurbulence type="fractalNoise" baseFrequency="0.012" numOctaves="4" seed="42" result="noise" />
                        <feDisplacementMap in="SourceGraphic" in2="noise" scale="180" result="displaced" />
                        <feGaussianBlur in="displaced" stdDeviation="4" />
                    </filter>
                    <filter id="c-mid" x="-100%" y="-100%" width="350%" height="350%">
                        <feTurbulence type="fractalNoise" baseFrequency="0.012" numOctaves="3" seed="43" result="noise" />
                        <feDisplacementMap in="SourceGraphic" in2="noise" scale="140" result="displaced" />
                        <feGaussianBlur in="displaced" stdDeviation="14" />
                    </filter>
                    <filter id="c-front" x="-100%" y="-100%" width="350%" height="350%">
                        <feTurbulence type="fractalNoise" baseFrequency="0.012" numOctaves="2" seed="44" result="noise" />
                        <feDisplacementMap in="SourceGraphic" in2="noise" scale="100" result="displaced" />
                        <feGaussianBlur in="displaced" stdDeviation="18" />
                    </filter>
                </defs>
            </svg>

            {/* ── Clouds — each is 3 layered divs (back/mid/front) ── */}
            <Cloud
                filterSet="a" width={500} height={275} opacity={0.95}
                className="top-[5%] left-[5%]"
                style={{ animation: "cloud-float-1 55s ease-in-out infinite" }}
            />
            <Cloud
                filterSet="b" width={450} height={250} opacity={0.9}
                className="top-[8%] left-[52%]"
                style={{ animation: "cloud-float-2 48s ease-in-out infinite" }}
            />
            <Cloud
                filterSet="c" width={380} height={210} opacity={0.85}
                className="top-[30%] left-[18%]"
                style={{ animation: "cloud-float-3 42s ease-in-out infinite" }}
            />
            <Cloud
                filterSet="a" width={420} height={230} opacity={0.7}
                className="top-[55%] left-[48%]"
                style={{ animation: "cloud-float-4 50s ease-in-out infinite" }}
            />
            <Cloud
                filterSet="b" width={320} height={175} opacity={0.6}
                className="top-[45%] left-[-2%]"
                style={{ animation: "cloud-float-1 44s ease-in-out infinite" }}
            />

            {/* ── Sun glow — pulsing warm bloom ── */}
            <div
                className="absolute top-[6%] right-[12%] w-[800px] h-[800px] rounded-full pointer-events-none blur-3xl z-[1]"
                style={{
                    background: "radial-gradient(circle, rgba(253,230,138,0.22) 0%, rgba(253,186,116,0.08) 35%, rgba(56,189,248,0.03) 55%, transparent 70%)",
                    animation: "sun-pulse 22s ease-in-out infinite",
                }}
            />

            {/* ═══════════════════════════════════════════
                Content layer
               ═══════════════════════════════════════════ */}
            <div className="relative z-10 h-full flex">

                {/* ── Left: Content floating on the sky (desktop) ── */}
                <div className="animate-fade-in hidden lg:flex w-[48%] relative flex-col justify-between py-12 pl-[8%] pr-12">

                    {/* Sky-colored veil — same blue as the background, softens clouds without darkening */}
                    <div
                        className="absolute inset-0 pointer-events-none"
                        style={{
                            background: `
                                radial-gradient(ellipse 130% 160% at 0% 50%, rgba(2,132,199,0.80) 0%, rgba(14,165,233,0.50) 40%, transparent 70%),
                                radial-gradient(ellipse 90% 80% at 20% 80%, rgba(3,105,161,0.55) 0%, transparent 60%)
                            `,
                        }}
                    />

                    {/* Top — Logo */}
                    <div className="relative">
                        <div className="flex items-center gap-4">
                            <Logo className="w-14 text-white" />
                            <span style={{ fontFamily: "var(--font-display)" }} className="font-extrabold text-[26px] tracking-tight text-white">Warmbly</span>
                        </div>
                    </div>

                    {/* Center — Heading + Features */}
                    <div className="relative">
                        <h1 className="font-serif text-[46px] leading-[1.08] text-white tracking-tight mb-5">
                            Your emails<br />deserve the<br />
                            <span className="text-sky-200 italic">inbox.</span>
                        </h1>
                        <p className="text-[15px] leading-relaxed text-white/60 mb-10 max-w-[340px]">
                            Automated email warmup that builds your sender reputation and keeps you out of spam.
                        </p>

                        {/* Feature separator */}
                        <div className="w-12 h-px bg-gradient-to-r from-white/25 to-transparent mb-8" />

                        <div className="space-y-3">
                            {features.map(({ icon: Icon, label, desc }) => (
                                <div key={label} className="flex items-start gap-3.5 group">
                                    <div className="mt-0.5 shrink-0 w-9 h-9 rounded-lg bg-white/[0.06] border border-white/[0.08] flex items-center justify-center">
                                        <Icon className="w-[18px] h-[18px] text-sky-200/90" />
                                    </div>
                                    <div>
                                        <p className="font-medium text-[14px] text-white/95">{label}</p>
                                        <p className="text-[13px] text-white/40 mt-0.5">{desc}</p>
                                    </div>
                                </div>
                            ))}
                        </div>
                    </div>

                    {/* Bottom — GitHub + trial */}
                    <div className="relative">
                        <div className="flex items-center gap-6">
                            <a
                                href="https://github.com/warmbly/warmbly"
                                target="_blank"
                                rel="noopener noreferrer"
                                className="flex items-center gap-2 text-[13px] text-white/50 hover:text-white/80 transition-colors"
                            >
                                <Github className="w-4 h-4" />
                                <span>Star on GitHub</span>
                                <ArrowRight className="w-3 h-3" />
                            </a>
                            <span className="text-white/15">|</span>
                            <span className="text-[13px] text-white/40">Apache 2.0</span>
                        </div>
                        <p className="text-[12px] text-white/30 mt-3">
                            14-day free trial · No credit card required
                        </p>
                        <div className="flex items-center gap-3 mt-2">
                            <a href={`${WEBSITE_URL}/terms`} target="_blank" rel="noopener noreferrer" className="text-[12px] text-white/30 hover:text-white/60 transition-colors">Terms of Service</a>
                            <span className="text-white/15">·</span>
                            <a href={`${WEBSITE_URL}/privacy`} target="_blank" rel="noopener noreferrer" className="text-[12px] text-white/30 hover:text-white/60 transition-colors">Privacy Policy</a>
                        </div>
                    </div>
                </div>

                {/* ── Right: Auth card ── */}
                <div className="flex-1 flex items-center justify-center px-5 py-12">
                    <div className="w-full max-w-[440px]">
                        <div className="lg:hidden flex items-center justify-center gap-2.5 mb-8 animate-fade-in">
                            <Logo className="w-10 text-white" />
                            <span style={{ fontFamily: "var(--font-display)" }} className="font-extrabold text-xl tracking-tight text-white">Warmbly</span>
                        </div>
                        <Outlet />
                    </div>
                </div>
            </div>
        </div>
    );
}
