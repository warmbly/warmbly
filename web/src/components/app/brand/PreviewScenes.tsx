// The pages BrandPreview draws. The sign-in form uses the dashboard's own utilities under the brand's
// theme, so it takes the same colours the real page does; the connect page and the report use the
// tones the client pages derive from the accent.

import React from "react";
import { ServerIcon } from "lucide-react";

import { DashboardImage } from "@/components/ui/dashboard-image";
import type { PortalTheme, PublicBrand } from "@/lib/api/models/app/brand/Brand";
import { CLIENT_SURFACES, clientPageTones, mixHex } from "@/lib/clientPageTones";
import { FONT_STACKS, brandImageURL, supportHref } from "@/lib/portalTheme";
import type { ResolvedTheme } from "@/lib/theme";
import { cn } from "@/lib/utils";

interface SceneProps {
    brand: PublicBrand;
    theme: PortalTheme;
    mode: ResolvedTheme;
    /** A phone's width. */
    compact?: boolean;
}

/** The form a sign-in page holds, as a still. */
export function SignInFormScene() {
    return (
        <div className="space-y-6">
            <div className="text-center">
                <div className="text-[28px] font-bold text-slate-900 tracking-tight leading-tight">Welcome back</div>
                <p className="text-sm text-slate-400 mt-1.5">Sign in to your account</p>
            </div>
            <div className="space-y-4">
                <div>
                    <div className="text-sm font-medium text-slate-600 pl-0.5">Email address</div>
                    <div className="mt-1 flex h-11 w-full items-center rounded-lg border border-sky-400 bg-white px-4 text-[15px] text-slate-900 ring-4 ring-sky-400/15">
                        alex@client.com
                    </div>
                </div>
                <div className="theme-solid flex h-11 w-full items-center justify-center rounded-lg bg-gradient-to-b from-sky-500 to-sky-600 text-[15px] font-semibold text-white shadow-[0_1px_2px_rgba(0,0,0,0.06),0_4px_12px_color-mix(in_oklab,var(--color-sky-500)_35%,transparent)]">
                    Continue
                </div>
            </div>
            <div className="text-center text-xs font-medium text-sky-500">Forgot your password?</div>
        </div>
    );
}

const CLIENT_RADIUS = {
    "": { card: 14, control: 7, button: 7 },
    sharp: { card: 0, control: 0, button: 0 },
    round: { card: 22, control: 12, button: 999 },
} as const;

const CLIENT_FONT = "'Inter Variable', Inter, system-ui, -apple-system, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif";

function useClientLook({ brand, theme, mode }: SceneProps) {
    const tones = clientPageTones(brand.accent_color, mode);
    const surface = CLIENT_SURFACES[mode];
    const radius = CLIENT_RADIUS[theme.corners ?? ""];
    const font = theme.font ? FONT_STACKS[theme.font] : CLIENT_FONT;
    return { tones, surface, radius, font };
}

function ClientMark({ brand, ink }: { brand: PublicBrand; ink: string }) {
    const logo = brandImageURL(brand.logo_url);
    if (logo) return <DashboardImage src={logo} alt="" className="h-7 max-w-[150px] object-contain" />;
    return (
        <span className="max-w-full truncate text-[16px] font-semibold tracking-tight" style={{ color: ink }}>
            {brand.name}
        </span>
    );
}

function ClientFooter({ brand, theme, muted, link }: { brand: PublicBrand; theme: PortalTheme; muted: string; link: string }) {
    const support = supportHref(theme);
    const meta = [theme.terms_url && "Terms", theme.privacy_url && "Privacy", brand.powered_by && `Powered by ${brand.powered_by.name}`].filter(
        (v): v is string => !!v,
    );
    if (!support && meta.length === 0) return null;
    return (
        <div className="mt-5 space-y-1 text-center text-[11.5px]" style={{ color: muted }}>
            {support && (
                <p>
                    Need help? <span style={{ color: link }}>{theme.support_url ? "Contact support" : theme.support_email}</span>
                </p>
            )}
            {meta.length > 0 && <p>{meta.join("  ·  ")}</p>}
        </div>
    );
}

// The providers' own marks, as the connect page shows them.
const GoogleMark = () => (
    <svg viewBox="0 0 24 24" className="size-4" aria-hidden>
        <path fill="#4285F4" d="M23.5 12.3c0-.8-.1-1.6-.2-2.3H12v4.4h6.5a5.5 5.5 0 0 1-2.4 3.6v3h3.9c2.300-2.100 3.500-5.200 3.500-8.700Z" />
        <path fill="#34A853" d="M12 24c3.200 0 6-1.100 8-2.900l-3.900-3c-1.100.700-2.500 1.200-4.100 1.200-3.100 0-5.800-2.100-6.700-5H1.300v3.100A12 12 0 0 0 12 24Z" />
        <path fill="#FBBC05" d="M5.300 14.300a7.200 7.200 0 0 1 0-4.600V6.600H1.300a12 12 0 0 0 0 10.800l4-3.100Z" />
        <path fill="#EA4335" d="M12 4.800c1.800 0 3.300.600 4.600 1.800l3.400-3.400A12 12 0 0 0 1.300 6.600l4 3.100c.900-2.900 3.600-4.900 6.700-4.900Z" />
    </svg>
);
const MicrosoftMark = () => (
    <svg viewBox="0 0 24 24" className="size-4" aria-hidden>
        <path fill="#F25022" d="M2 2h9.500v9.500H2z" />
        <path fill="#7FBA00" d="M12.500 2H22v9.500h-9.500z" />
        <path fill="#00A4EF" d="M2 12.500h9.500V22H2z" />
        <path fill="#FFB900" d="M12.500 12.500H22V22h-9.500z" />
    </svg>
);

const PROVIDERS = [
    { label: "Continue with Google", mark: <GoogleMark /> },
    { label: "Continue with Microsoft", mark: <MicrosoftMark /> },
    { label: "Use server details", mark: <ServerIcon className="size-4 opacity-70" /> },
];

/** The page a client opens from a connect link. */
export function ConnectScene(props: SceneProps) {
    const { brand, theme } = props;
    const { tones, surface, radius, font } = useClientLook(props);
    return (
        <div className="flex h-full w-full flex-col items-center justify-center overflow-hidden px-5 py-9" style={{ background: surface.page, color: surface.ink, fontFamily: font }}>
            <div className="mb-5 flex max-w-full justify-center">
                <ClientMark brand={brand} ink={surface.ink} />
            </div>
            <div
                className="w-full max-w-[440px] p-6"
                style={{
                    background: surface.card,
                    border: `1px solid ${surface.line}`,
                    borderRadius: radius.card,
                    boxShadow: "0 1px 2px rgba(15,23,42,0.05), 0 18px 40px -22px rgba(15,23,42,0.28)",
                }}
            >
                <div className="text-[19px] font-semibold tracking-tight">Connect your mailbox to Acme</div>
                <p className="mt-1.5 text-[13.5px] leading-relaxed" style={{ color: surface.muted }}>
                    Sign in with your email provider. Your password is never shared.
                </p>
                <div className="mt-4">
                    <div className="flex justify-between text-[12px]" style={{ color: surface.muted }}>
                        <span>2 of 5 mailboxes connected</span>
                        <span style={{ color: tones.link }}>See them</span>
                    </div>
                    <div className="mt-1.5 h-1.5 overflow-hidden" style={{ background: surface.line, borderRadius: radius.button }}>
                        <div className="h-full w-2/5" style={{ background: tones.chart, borderRadius: radius.button }} />
                    </div>
                </div>
                <div className="mt-5 space-y-2">
                    {PROVIDERS.map((p, i) => (
                        <div
                            key={p.label}
                            className="flex h-11 items-center justify-center gap-2.5 text-[13.5px] font-medium"
                            style={
                                i === 0
                                    ? { background: tones.accent, color: tones.onAccent, borderRadius: radius.button }
                                    : { border: `1px solid ${surface.line}`, color: surface.ink, borderRadius: radius.button }
                            }
                        >
                            {/* On the accent the mark sits on its own white disc, as it does on the page. */}
                            <span className={cn("inline-flex items-center justify-center", i === 0 && "size-6 rounded-full bg-white")}>{p.mark}</span>
                            {p.label}
                        </div>
                    ))}
                </div>
            </div>
            <ClientFooter brand={brand} theme={theme} muted={surface.muted} link={tones.link} />
        </div>
    );
}

const BARS = [34, 48, 41, 62, 55, 73, 66, 58, 79, 70, 84, 76];

/** A report shared with a client. */
export function ReportScene(props: SceneProps) {
    const { brand, theme, mode, compact = false } = props;
    const { tones, surface, radius, font } = useClientLook(props);
    const card: React.CSSProperties = { background: surface.card, border: `1px solid ${surface.line}`, borderRadius: radius.card };
    const stats = [
        ["Emails sent", "1,240"],
        ["Opens", "61%"],
        ["Replies", "38"],
        ["Reply rate", "3.1%"],
    ];
    return (
        <div className="h-full w-full overflow-hidden px-6 py-7" style={{ background: surface.page, color: surface.ink, fontFamily: font }}>
            <div className="mx-auto max-w-[760px]">
                <div className="flex items-center justify-between gap-4">
                    <ClientMark brand={brand} ink={surface.ink} />
                    <span className="shrink-0 px-2.5 py-1 text-[11.5px] font-medium" style={{ background: tones.soft, color: tones.link, borderRadius: radius.button }}>
                        Last 30 days
                    </span>
                </div>
                <div className="mt-5 text-[21px] font-semibold tracking-tight">Outreach report for Acme</div>
                <p className="mt-1 text-[13px]" style={{ color: surface.muted }}>
                    Three campaigns, twelve mailboxes.
                </p>
                <div className={cn("mt-5 grid gap-3", compact ? "grid-cols-2" : "grid-cols-4")}>
                    {stats.map(([label, value]) => (
                        <div key={label} className="px-3.5 py-3" style={card}>
                            <div className="text-[10.5px] uppercase tracking-[0.12em]" style={{ color: surface.muted }}>
                                {label}
                            </div>
                            <div className="mt-1 text-[22px] font-semibold tracking-tight">{value}</div>
                        </div>
                    ))}
                </div>
                <div className="mt-3 px-4 py-4" style={card}>
                    <div className="flex items-center justify-between text-[12.5px]">
                        <span className="font-medium">Replies by week</span>
                        <span style={{ color: tones.link }}>View all</span>
                    </div>
                    <div className="mt-4 flex h-28 items-end gap-2">
                        {BARS.slice(0, compact ? 7 : 12).map((h, i) => (
                            <div
                                key={i}
                                className="flex-1"
                                style={{
                                    height: `${h}%`,
                                    background: i % 3 === 2 ? tones.chart : mixHex(surface.card, tones.chart, mode === "dark" ? 0.55 : 0.45),
                                    borderRadius: Math.min(radius.control, 4),
                                }}
                            />
                        ))}
                    </div>
                </div>
                <ClientFooter brand={brand} theme={theme} muted={surface.muted} link={tones.link} />
            </div>
        </div>
    );
}
