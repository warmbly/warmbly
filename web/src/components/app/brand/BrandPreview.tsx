// BrandPreview: what a brand looks like where people meet it, drawn live from the editor's draft:
// the portal's sign-in page (the real component), the dashboard, a connect page and a shared report.

import React from "react";
import { LockIcon, MonitorIcon, MoonIcon, SmartphoneIcon, SunIcon } from "lucide-react";

import ScrollStrip from "@/components/ui/scroll-strip";
import PortalSignIn from "@/components/shared/PortalSignIn";
import type { PublicBrand } from "@/lib/api/models/app/brand/Brand";
import { loadThemeFont, parsePortalTheme } from "@/lib/portalTheme";
import type { ResolvedTheme } from "@/lib/theme";
import { cn } from "@/lib/utils";
import { ConnectScene, DashboardScene, ReportScene, SignInFormScene } from "./PreviewScenes";

export type PreviewTab = "signin" | "dashboard" | "connect" | "report";
type Device = "desktop" | "phone";

const TAB_LABELS: Record<PreviewTab, string> = {
    signin: "Sign-in",
    dashboard: "Dashboard",
    connect: "Connect page",
    report: "Report",
};

const CAPTIONS: Record<PreviewTab, string> = {
    signin: "The sign-in page on your portal domain.",
    dashboard: "The dashboard your clients work in.",
    connect: "The page a client opens to connect a mailbox.",
    report: "A report shared with a client.",
};

// The page each view is drawn at before it is scaled to fit: real page widths, so each lays out as it does in a browser.
const PHONE = { width: 390, height: 780 };
const DESKTOP: Record<PreviewTab, { width: number; height: number }> = {
    signin: { width: 1180, height: 720 },
    dashboard: { width: 1040, height: 640 },
    connect: { width: 1040, height: 640 },
    report: { width: 1040, height: 640 },
};

// Where each page lives, for the address bar.
const PATHS: Record<PreviewTab, string> = {
    signin: "/auth/login",
    dashboard: "/app/campaigns",
    connect: "/connect/k3f9a2",
    report: "/r/q7m2x8",
};

const ALL_PREVIEW_TABS: PreviewTab[] = ["signin", "dashboard", "connect", "report"];

export default function BrandPreview({
    brand,
    fallbackName,
    tabs = ALL_PREVIEW_TABS,
    tab,
    onTabChange,
    address,
}: {
    brand: PublicBrand;
    /** Shown when the brand has no name. */
    fallbackName: string;
    tabs?: PreviewTab[];
    /** The tab shown, when the editor steers it. */
    tab?: PreviewTab;
    onTabChange?: (tab: PreviewTab) => void;
    /** The host the pages are served on; a stand-in is shown without one. */
    address?: string;
}) {
    const [ownTab, setOwnTab] = React.useState<PreviewTab>(tabs[0]);
    const shownTab = tab && tabs.includes(tab) ? tab : tabs.includes(ownTab) ? ownTab : tabs[0];
    const pick = (t: PreviewTab) => {
        setOwnTab(t);
        onTabChange?.(t);
    };

    const theme = React.useMemo(() => parsePortalTheme(brand.theme), [brand.theme]);
    // The preview opens in the brand's default appearance and follows it until the viewer flips it here.
    const preferred: ResolvedTheme = theme.appearance || "light";
    const [flipped, setFlipped] = React.useState<{ from: ResolvedTheme; to: ResolvedTheme } | null>(null);
    const mode = flipped && flipped.from === preferred ? flipped.to : preferred;
    const [device, setDevice] = React.useState<Device>("desktop");

    const named: PublicBrand = { ...brand, name: brand.name || fallbackName, theme };
    const view = device === "phone" ? PHONE : DESKTOP[shownTab];
    // A draft's font is drawn here before it is ever applied to a page.
    React.useEffect(() => {
        void loadThemeFont(theme.font);
    }, [theme.font]);

    return (
        <div className="overflow-hidden rounded-xl border border-slate-200 bg-white shadow-[0_1px_2px_rgba(15,23,42,0.04)]">
            <div className="flex items-center gap-2 border-b border-slate-200 px-2 py-1.5">
                <ScrollStrip activeKey={shownTab} className="min-w-0 flex-1">
                    <div className="inline-flex items-center gap-0.5 rounded-lg bg-slate-100 p-0.5">
                        {tabs.map((t) => (
                            <button
                                key={t}
                                type="button"
                                data-active={shownTab === t || undefined}
                                aria-pressed={shownTab === t}
                                onClick={() => pick(t)}
                                className={cn(
                                    "h-7 shrink-0 rounded-md px-2.5 text-[12.5px] whitespace-nowrap outline-none transition-colors focus-visible:ring-2 focus-visible:ring-sky-200",
                                    shownTab === t
                                        ? "bg-white font-medium text-slate-900 shadow-[0_1px_2px_rgba(15,23,42,0.08)] ring-1 ring-slate-200/70"
                                        : "text-slate-500 hover:text-slate-800",
                                )}
                            >
                                {TAB_LABELS[t]}
                            </button>
                        ))}
                    </div>
                </ScrollStrip>
                <IconSwitch
                    label="Screen size"
                    value={device}
                    onChange={setDevice}
                    options={[
                        { value: "desktop", label: "Desktop", icon: MonitorIcon },
                        { value: "phone", label: "Phone", icon: SmartphoneIcon },
                    ]}
                />
                <IconSwitch
                    label="Appearance"
                    value={mode}
                    onChange={(to) => setFlipped({ from: preferred, to })}
                    options={[
                        { value: "light", label: "Light", icon: SunIcon },
                        { value: "dark", label: "Dark", icon: MoonIcon },
                    ]}
                />
            </div>

            {/* The canvas: a dotted desk the page sits on. */}
            <div className="bg-slate-100 bg-[radial-gradient(rgba(15,23,42,0.07)_1px,transparent_1px)] [background-size:14px_14px] px-4 py-5 md:px-6 md:py-7">
                <Stage
                    width={view.width}
                    height={view.height}
                    phone={device === "phone"}
                    dark={mode === "dark"}
                    url={`${address || "portal.youragency.com"}${PATHS[shownTab]}`}
                >
                    {shownTab === "signin" ? (
                        <PortalSignIn brand={named} scope={mode} contained>
                            <SignInFormScene />
                        </PortalSignIn>
                    ) : shownTab === "dashboard" ? (
                        <DashboardScene brand={named} theme={theme} mode={mode} compact={device === "phone"} />
                    ) : shownTab === "connect" ? (
                        <ConnectScene brand={named} theme={theme} mode={mode} />
                    ) : (
                        <ReportScene brand={named} theme={theme} mode={mode} compact={device === "phone"} />
                    )}
                </Stage>
                <p className="mt-4 text-center text-[11.5px] text-slate-500">{CAPTIONS[shownTab]}</p>
            </div>
        </div>
    );
}

// The browser bar above a page, or nothing on a phone.
const CHROME = 34;

// How tall the framed page may be: the window, less the app's own bars and the preview's.
function useRoomBelow(): number {
    const read = () => (typeof window === "undefined" ? 720 : Math.max(360, window.innerHeight - 250));
    const [room, setRoom] = React.useState(read);
    React.useEffect(() => {
        const onResize = () => setRoom(read());
        window.addEventListener("resize", onResize);
        return () => window.removeEventListener("resize", onResize);
    }, []);
    return room;
}

// Draws its children at a fixed page size inside a browser or a phone, scaled to the room there is and never enlarged.
function Stage({
    width,
    height,
    phone,
    dark,
    url,
    children,
}: {
    width: number;
    height: number;
    phone: boolean;
    dark: boolean;
    url: string;
    children: React.ReactNode;
}) {
    const ref = React.useRef<HTMLDivElement>(null);
    const [room, setRoom] = React.useState(0);
    const tall = useRoomBelow();
    React.useLayoutEffect(() => {
        const el = ref.current;
        if (!el) return;
        const measure = () => setRoom(el.clientWidth);
        measure();
        const ro = new ResizeObserver(measure);
        ro.observe(el);
        return () => ro.disconnect();
    }, []);

    const bezel = phone ? 16 : 0;
    const bar = phone ? 0 : CHROME;
    const scale = room ? Math.min((room - bezel) / width, (tall - bar - bezel) / height, 1) : 0;
    return (
        <div ref={ref} className="flex justify-center">
            <div
                className={cn(
                    "shrink-0 overflow-hidden shadow-[0_1px_2px_rgba(15,23,42,0.06),0_24px_48px_-24px_rgba(15,23,42,0.35)]",
                    phone ? "rounded-[30px] bg-slate-900 p-2 ring-1 ring-slate-900/20" : "rounded-lg bg-white ring-1 ring-slate-900/10",
                )}
            >
                {!phone && (
                    <div
                        aria-hidden
                        className={cn("flex items-center gap-3 border-b px-3", dark ? "border-white/10 bg-[#1b1d21]" : "border-slate-200 bg-slate-50")}
                        style={{ height: CHROME, width: width * scale }}
                    >
                        <span className="flex shrink-0 gap-1.5">
                            {["bg-[#ff5f57]", "bg-[#febc2e]", "bg-[#28c840]"].map((c) => (
                                <span key={c} className={cn("size-2.5 rounded-full", c)} />
                            ))}
                        </span>
                        <span
                            className={cn(
                                "mx-auto flex h-[22px] min-w-0 max-w-[420px] flex-1 items-center gap-1.5 rounded-md px-2.5 text-[11.5px]",
                                dark ? "bg-white/10 text-slate-300" : "bg-white text-slate-500 ring-1 ring-slate-200",
                            )}
                        >
                            <LockIcon className="size-3 shrink-0 opacity-70" />
                            <span className="truncate">{url}</span>
                        </span>
                        <span className="w-[42px] shrink-0" />
                    </div>
                )}
                <div className={cn("relative overflow-hidden bg-white", phone && "rounded-[22px]")} style={{ width: width * scale, height: height * scale }}>
                    {scale > 0 && (
                        // The miniature is a picture: nothing in it takes focus or a click.
                        <div
                            inert
                            aria-hidden
                            className="absolute left-0 top-0 origin-top-left select-none"
                            style={{ width, height, transform: `scale(${scale})` }}
                        >
                            {children}
                        </div>
                    )}
                </div>
            </div>
        </div>
    );
}

function IconSwitch<T extends string>({
    label,
    value,
    onChange,
    options,
}: {
    label: string;
    value: T;
    onChange: (v: T) => void;
    options: { value: T; label: string; icon: React.ComponentType<{ className?: string }> }[];
}) {
    return (
        <div role="radiogroup" aria-label={label} className="inline-flex shrink-0 items-center gap-0.5 rounded-md bg-slate-100 p-0.5">
            {options.map(({ value: v, label: l, icon: Icon }) => {
                const active = v === value;
                return (
                    <button
                        key={v}
                        type="button"
                        role="radio"
                        aria-checked={active}
                        aria-label={l}
                        title={l}
                        onClick={() => onChange(v)}
                        className={cn(
                            "inline-flex h-6 w-7 items-center justify-center rounded-[5px] outline-none transition-colors focus-visible:ring-2 focus-visible:ring-sky-200",
                            active
                                ? "bg-white text-slate-900 shadow-[0_1px_2px_rgba(15,23,42,0.08)] ring-1 ring-slate-200/70"
                                : "text-slate-400 hover:text-slate-700",
                        )}
                    >
                        <Icon className="size-3.5" />
                    </button>
                );
            })}
        </div>
    );
}
