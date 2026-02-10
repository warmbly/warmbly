import React from "react";
import { Outlet, useNavigate } from "react-router-dom";
import { APP_URL } from "@/lib/information";
import { Logo } from "@/components/svg";
import { Mail, BarChart3, Zap, Shield, Github, ArrowRight } from "lucide-react";

/* ═══════════════════════════════════════════
   Realistic Cloud
   feTurbulence + feDisplacementMap create organic
   Perlin-noise edges on a CSS box-shadow.
   mix-blend-mode: screen hides the black source.
   ═══════════════════════════════════════════ */

function Cloud({
    className,
    style,
    filterId,
    width,
    height,
    blur = 50,
    opacity = 0.9,
}: {
    className?: string;
    style?: React.CSSProperties;
    filterId: string;
    width: number;
    height: number;
    blur?: number;
    opacity?: number;
}) {
    const sx = Math.round(width * 0.8);
    const sy = Math.round(height * 0.9);

    return (
        <div
            className={`absolute pointer-events-none select-none ${className ?? ""}`}
            style={{ ...style, mixBlendMode: "screen" as const }}
        >
            <div
                style={{
                    position: "absolute",
                    top: -sy,
                    left: -sx,
                    width,
                    height,
                    background: "#000",
                    borderRadius: "50%",
                    filter: `url(#${filterId})`,
                    boxShadow: [
                        `${sx}px ${sy}px ${blur}px 0px rgba(255,255,255,${opacity})`,
                        `${sx}px ${sy + Math.round(height * 0.06)}px ${Math.round(blur * 0.3)}px -2px rgba(186,230,253,${(opacity * 0.12).toFixed(2)})`,
                    ].join(", "),
                    pointerEvents: "none",
                }}
            />
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
   Auth Layout — Radial sky
   The gradient radiates from the upper-right (the "sun")
   creating naturally deeper blue on the left for text
   and brighter sky on the right for the card.
   ═══════════════════════════════════════════ */

export default function AuthLayout() {
    const navigate = useNavigate();

    React.useEffect(() => {
        const receiveMessage = (event: MessageEvent) => {
            if (event.origin !== APP_URL) return;
            if (event.data?.type === "auth") navigate("/app/emails");
        };
        window.addEventListener("message", receiveMessage);
        return () => window.removeEventListener("message", receiveMessage);
    }, [navigate]);

    return (
        <div
            className="relative min-h-screen overflow-hidden"
            style={{
                background: "radial-gradient(ellipse 140% 140% at 72% 25%, #38bdf8 0%, #0ea5e9 18%, #0284c7 36%, #075985 58%, #0c4a6e 82%)",
            }}
        >

            {/* ── SVG cloud filters ── */}
            <svg className="absolute" width="0" height="0" aria-hidden="true">
                <filter id="cloud-a" x="-100%" y="-100%" width="350%" height="350%">
                    <feTurbulence type="fractalNoise" baseFrequency="0.012" numOctaves="4" seed="3" />
                    <feDisplacementMap in="SourceGraphic" scale="170" />
                </filter>
                <filter id="cloud-b" x="-100%" y="-100%" width="350%" height="350%">
                    <feTurbulence type="fractalNoise" baseFrequency="0.012" numOctaves="3" seed="17" />
                    <feDisplacementMap in="SourceGraphic" scale="130" />
                </filter>
                <filter id="cloud-c" x="-100%" y="-100%" width="350%" height="350%">
                    <feTurbulence type="fractalNoise" baseFrequency="0.015" numOctaves="3" seed="42" />
                    <feDisplacementMap in="SourceGraphic" scale="100" />
                </filter>
                <filter id="cloud-d" x="-100%" y="-100%" width="350%" height="350%">
                    <feTurbulence type="fractalNoise" baseFrequency="0.018" numOctaves="2" seed="88" />
                    <feDisplacementMap in="SourceGraphic" scale="60" />
                </filter>
            </svg>

            {/* ── Clouds ── */}
            <Cloud
                filterId="cloud-a" width={520} height={280} blur={60} opacity={0.82}
                className="top-[2%] left-[5%]"
                style={{ animation: "cloud-float-1 50s ease-in-out infinite" }}
            />
            <Cloud
                filterId="cloud-b" width={380} height={200} blur={45} opacity={0.72}
                className="top-[10%] left-[55%]"
                style={{ animation: "cloud-float-2 40s ease-in-out infinite" }}
            />
            <Cloud
                filterId="cloud-c" width={240} height={130} blur={32} opacity={0.60}
                className="top-[35%] left-[18%]"
                style={{ animation: "cloud-float-3 35s ease-in-out infinite" }}
            />
            <Cloud
                filterId="cloud-a" width={440} height={240} blur={55} opacity={0.50}
                className="top-[60%] left-[48%]"
                style={{ animation: "cloud-float-2 58s ease-in-out infinite" }}
            />
            <Cloud
                filterId="cloud-d" width={280} height={80} blur={22} opacity={0.38}
                className="top-[50%] left-[0%]"
                style={{ animation: "cloud-float-3 48s ease-in-out infinite" }}
            />
            <Cloud
                filterId="cloud-c" width={200} height={110} blur={28} opacity={0.45}
                className="top-[55%] left-[75%]"
                style={{ animation: "cloud-float-1 42s ease-in-out infinite" }}
            />

            {/* ── Sun glow — warm bloom near the radial center ── */}
            <div
                className="absolute top-[8%] right-[14%] w-[700px] h-[700px] rounded-full pointer-events-none blur-3xl z-[1]"
                style={{
                    background: "radial-gradient(circle, rgba(253,230,138,0.18) 0%, rgba(253,186,116,0.06) 40%, transparent 65%)",
                }}
            />

            {/* ═══════════════════════════════════════════
                Content layer
               ═══════════════════════════════════════════ */}
            <div className="relative z-10 min-h-screen flex">

                {/* ── Left: Brand content (desktop) ── */}
                <div className="hidden lg:flex w-[48%] flex-col justify-between py-12 pl-[8%] pr-16">

                    {/* Top — Logo */}
                    <div className="animate-fade-in">
                        <div className="flex items-center gap-5">
                            <div>
                                <Logo className="w-20" />
                            </div>
                            <span className="font-sans font-bold text-2xl tracking-tight text-white/90">Warmbly</span>
                        </div>
                    </div>

                    {/* Center — Heading + Features */}
                    <div className="animate-fade-in">
                        <h1 className="font-serif text-[46px] leading-[1.08] text-white tracking-tight mb-5">
                            Your emails<br />deserve the<br />
                            <span className="text-sky-200 italic">inbox.</span>
                        </h1>
                        <p className="text-[15px] leading-relaxed text-white/50 mb-12 max-w-[340px]">
                            Automated email warmup that builds your sender reputation and keeps you out of spam.
                        </p>

                        <div className="space-y-3">
                            {features.map(({ icon: Icon, label, desc }) => (
                                <div key={label} className="flex items-start gap-3.5">
                                    <div className="mt-0.5 shrink-0 w-9 h-9 rounded-lg bg-white/[0.08] border border-white/[0.08] flex items-center justify-center">
                                        <Icon className="w-[18px] h-[18px] text-sky-200/80" />
                                    </div>
                                    <div>
                                        <p className="font-medium text-[14px] text-white/90">{label}</p>
                                        <p className="text-[13px] text-white/35 mt-0.5">{desc}</p>
                                    </div>
                                </div>
                            ))}
                        </div>
                    </div>

                    {/* Bottom — GitHub + trial */}
                    <div className="animate-fade-in">
                        <div className="flex items-center gap-6">
                            <a
                                href="https://github.com/warmbly/warmbly"
                                target="_blank"
                                rel="noopener noreferrer"
                                className="flex items-center gap-2 text-[13px] text-white/45 hover:text-white/75 transition-colors"
                            >
                                <Github className="w-4 h-4" />
                                <span>Star on GitHub</span>
                                <ArrowRight className="w-3 h-3" />
                            </a>
                            <span className="text-white/15">|</span>
                            <span className="text-[13px] text-white/35">Apache 2.0</span>
                        </div>
                        <p className="text-[12px] text-white/25 mt-3">
                            14-day free trial · No credit card required
                        </p>
                    </div>
                </div>

                {/* ── Right: Auth card ── */}
                <div className="flex-1 flex items-center justify-center px-5 py-12">
                    <div className="w-full max-w-[440px]">
                        <div className="lg:hidden flex items-center justify-center gap-2.5 mb-8 animate-fade-in">
                            <div style={{ filter: "brightness(0) invert(1)" }}>
                                <Logo className="w-8" />
                            </div>
                            <span className="font-sans font-bold text-xl tracking-tight text-white">Warmbly</span>
                        </div>
                        <Outlet />
                    </div>
                </div>
            </div>
        </div>
    );
}
