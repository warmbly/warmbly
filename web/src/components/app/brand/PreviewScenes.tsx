// The pages BrandPreview draws. The dashboard and the sign-in form use the dashboard's own
// utilities under the brand's theme, so they take the same colours the real pages do; the connect
// page and the report use the tones the client pages derive from the accent.

import React from "react";
import {
    BarChart3Icon,
    CheckIcon,
    ContactIcon,
    InboxIcon,
    MailIcon,
    PlusIcon,
    SearchIcon,
    SendIcon,
    SettingsIcon,
} from "lucide-react";

import { DashboardImage } from "@/components/ui/dashboard-image";
import type { PortalTheme, PublicBrand } from "@/lib/api/models/app/brand/Brand";
import { isHexColour } from "@/lib/accentScale";
import { CLIENT_SURFACES, clientPageTones, mixHex } from "@/lib/clientPageTones";
import { FONT_STACKS, brandImageURL, scopedVariables, supportHref } from "@/lib/portalTheme";
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

function Mark({ brand, className }: { brand: PublicBrand; className?: string }) {
    const logo = brandImageURL(brand.logo_url);
    return (
        <span className={cn("flex min-w-0 items-center gap-2", className)}>
            {logo ? (
                <DashboardImage src={logo} alt="" className="h-6 max-w-[110px] shrink-0 object-contain" />
            ) : (
                <span aria-hidden className="inline-flex size-6 shrink-0 items-center justify-center rounded-md bg-sky-600 text-[12px] font-bold text-white">
                    {(brand.name || "?").slice(0, 1).toUpperCase()}
                </span>
            )}
            <span className="truncate text-[14px] font-semibold tracking-tight text-slate-900">{brand.name}</span>
        </span>
    );
}

const NAV = [
    { label: "Mailboxes", icon: MailIcon },
    { label: "Campaigns", icon: SendIcon, active: true },
    { label: "Inbox", icon: InboxIcon, count: 4 },
    { label: "Contacts", icon: ContactIcon },
    { label: "Analytics", icon: BarChart3Icon },
    { label: "Settings", icon: SettingsIcon },
];

const CAMPAIGNS = [
    { name: "Spring outreach", status: "Active", sent: 72, on: true, picked: true },
    { name: "Agency owners, UK", status: "Active", sent: 44, on: true, picked: false },
    { name: "Webinar follow-up", status: "Paused", sent: 61, on: false, picked: false },
    { name: "Re-engage lost deals", status: "Draft", sent: 0, on: false, picked: false },
];

/** The dashboard in a brand's theme: its mark in the chrome and its accent on everything that is sky. */
export function DashboardScene({ brand, theme, mode, compact = false }: SceneProps) {
    const accent = isHexColour(brand.accent_color) ? brand.accent_color : undefined;
    const style = React.useMemo(() => scopedVariables(accent, theme, mode) as React.CSSProperties, [accent, theme, mode]);
    return (
        <div
            style={style}
            className={cn("h-full w-full font-[family-name:var(--wb-font)] text-slate-900", mode === "dark" ? "dark" : "theme-light")}
        >
            <div className="flex h-full flex-col bg-[#f5f6f8] dark:bg-[var(--wb-chrome)]">
                <div className="flex h-12 shrink-0 items-center gap-3 px-4">
                    <Mark brand={brand} className="max-w-[55%]" />
                    {!compact && (
                        <span className="ml-auto flex h-7 w-44 items-center gap-1.5 rounded-md border border-slate-200 bg-white px-2 text-[12px] text-slate-400">
                            <SearchIcon className="size-3.5" />
                            Search
                        </span>
                    )}
                    <span className={cn("inline-flex size-7 shrink-0 items-center justify-center rounded-full bg-sky-100 text-[11px] font-semibold text-sky-700", compact && "ml-auto")}>
                        AK
                    </span>
                </div>
                <div className="flex min-h-0 flex-1">
                    {!compact && (
                        <div className="w-[176px] shrink-0 space-y-0.5 px-2.5 py-2">
                            {NAV.map(({ label, icon: Icon, active, count }) => (
                                <div
                                    key={label}
                                    className={cn(
                                        "flex h-8 items-center gap-2 rounded-md px-2 text-[12.5px]",
                                        active ? "wb-nav-active bg-white font-medium text-slate-900 shadow-sm" : "text-slate-600",
                                    )}
                                >
                                    <Icon className={cn("size-3.5", active ? "text-sky-600" : "text-slate-400")} />
                                    {label}
                                    {count && (
                                        <span className="ml-auto rounded-full bg-sky-600 px-1.5 text-[10px] font-semibold leading-4 text-white">{count}</span>
                                    )}
                                </div>
                            ))}
                        </div>
                    )}
                    <div className={cn("min-w-0 flex-1 overflow-hidden border-t border-slate-200/70 bg-white", !compact && "rounded-tl-2xl border-l")}>
                        <div className="flex h-12 items-center gap-2 border-b border-slate-200 px-4">
                            <span className="text-[14px] font-semibold tracking-tight text-slate-900">Campaigns</span>
                            <span className="rounded bg-slate-100 px-1.5 text-[11px] leading-5 text-slate-500">4</span>
                            <span className="ml-auto inline-flex h-7 items-center gap-1 rounded-md bg-sky-600 px-2.5 text-[12px] font-medium text-white">
                                <PlusIcon className="size-3.5" />
                                New campaign
                            </span>
                        </div>
                        <div className="flex items-center gap-1 border-b border-slate-200 px-3">
                            {["All", "Active", "Paused", "Drafts"].map((t, i) => (
                                <span key={t} className={cn("relative inline-flex h-10 items-center px-2.5 text-[12.5px]", i === 0 ? "font-medium text-slate-900" : "text-slate-500")}>
                                    {t}
                                    {i === 0 && <span className="absolute -bottom-px left-2 right-2 h-0.5 rounded-full bg-sky-600" />}
                                </span>
                            ))}
                        </div>
                        <div className={cn("grid gap-3 p-4", compact ? "grid-cols-2" : "grid-cols-3")}>
                            {[
                                ["Sent this week", "1,240", 68],
                                ["Replies", "38", 42],
                                ["Reply rate", "3.1%", 31],
                            ]
                                .slice(0, compact ? 2 : 3)
                                .map(([label, value, share]) => (
                                    <div key={label} className="rounded-lg border border-slate-200 px-3 py-2.5">
                                        <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400">{label}</div>
                                        <div className="mt-0.5 text-[18px] font-semibold tracking-tight text-slate-900">{value}</div>
                                        <div className="mt-2 h-1 overflow-hidden rounded-full bg-slate-100">
                                            <div className="h-full rounded-full bg-sky-500" style={{ width: `${share}%` }} />
                                        </div>
                                    </div>
                                ))}
                        </div>
                        <div className="mx-4 overflow-hidden rounded-md border border-slate-200">
                            {CAMPAIGNS.map((c) => (
                                <div
                                    key={c.name}
                                    className={cn("flex h-10 items-center gap-3 border-b border-slate-100 px-3 text-[12.5px] last:border-b-0", c.picked && "bg-sky-50")}
                                >
                                    <span
                                        className={cn(
                                            "inline-flex size-3.5 shrink-0 items-center justify-center rounded-[4px] border",
                                            c.picked ? "theme-solid border-sky-600 bg-sky-600 text-white" : "border-slate-300 bg-white",
                                        )}
                                    >
                                        {c.picked && <CheckIcon className="size-2.5" strokeWidth={3} />}
                                    </span>
                                    <span className="min-w-0 flex-1 truncate font-medium text-slate-900">{c.name}</span>
                                    {!compact && (
                                        <span className="h-1 w-20 overflow-hidden rounded-full bg-slate-100">
                                            <span className="block h-full rounded-full bg-sky-500" style={{ width: `${c.sent}%` }} />
                                        </span>
                                    )}
                                    <span
                                        className={cn(
                                            "rounded px-1.5 text-[11px] font-medium leading-5",
                                            c.status === "Active" ? "bg-sky-50 text-sky-700" : "bg-slate-100 text-slate-500",
                                            c.picked && c.status === "Active" && "bg-sky-100",
                                        )}
                                    >
                                        {c.status}
                                    </span>
                                    <span className={cn("relative inline-flex h-[18px] w-8 shrink-0 items-center rounded-full", c.on ? "bg-sky-600" : "bg-slate-200")}>
                                        <span className={cn("inline-block size-3.5 rounded-full bg-white shadow-sm", c.on ? "translate-x-[15px]" : "translate-x-[2px]")} />
                                    </span>
                                </div>
                            ))}
                        </div>
                        <div className="px-4 pt-3 text-[12px] text-slate-500">
                            Three campaigns are sending. <span className="font-medium text-sky-600">See the schedule</span>
                        </div>
                    </div>
                </div>
            </div>
        </div>
    );
}

// The client pages' corner scale, as portal/src/styles.css sets it per choice.
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

const PROVIDERS = [
    { label: "Continue with Google", dot: "#ea4335" },
    { label: "Continue with Microsoft", dot: "#0078d4" },
    { label: "Use server details", dot: "#64748b" },
];

/** The page a client opens from a connect link. */
export function ConnectScene(props: SceneProps) {
    const { brand, theme } = props;
    const { tones, surface, radius, font } = useClientLook(props);
    return (
        <div className="flex h-full w-full flex-col items-center overflow-hidden px-5 py-9" style={{ background: surface.page, color: surface.ink, fontFamily: font }}>
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
                            <span className="size-2.5 rounded-full" style={{ background: i === 0 ? tones.onAccent : p.dot }} />
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
