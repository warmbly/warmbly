// BrandPreview: what a brand looks like where people meet it, drawn live from the editor's draft:
// the portal's sign-in page (the real component), the dashboard, a connect page and a shared report.

import React from "react";
import { MonitorIcon, MoonIcon, SmartphoneIcon, SunIcon } from "lucide-react";

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

// The page each view is drawn at before it is scaled to fit. The sign-in page needs a real desktop
// width to lay out as one; the rest are drawn smaller so their text stays legible in miniature.
const PHONE = { width: 390, height: 760 };
const DESKTOP: Record<PreviewTab, { width: number; height: number }> = {
    signin: { width: 1040, height: 660 },
    dashboard: { width: 820, height: 520 },
    connect: { width: 820, height: 520 },
    report: { width: 820, height: 520 },
};

const ALL_PREVIEW_TABS: PreviewTab[] = ["signin", "dashboard", "connect", "report"];

export default function BrandPreview({
    brand,
    fallbackName,
    tabs = ALL_PREVIEW_TABS,
    tab,
    onTabChange,
}: {
    brand: PublicBrand;
    /** Shown when the brand has no name. */
    fallbackName: string;
    tabs?: PreviewTab[];
    /** The tab shown, when the editor steers it. */
    tab?: PreviewTab;
    onTabChange?: (tab: PreviewTab) => void;
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
        <div className="rounded-lg border border-slate-200 bg-white overflow-hidden">
            <div className="flex items-center gap-2 border-b border-slate-200 pl-2 pr-2">
                <ScrollStrip activeKey={shownTab} className="min-w-0 flex-1">
                    {tabs.map((t) => (
                        <button
                            key={t}
                            type="button"
                            data-active={shownTab === t || undefined}
                            aria-pressed={shownTab === t}
                            onClick={() => pick(t)}
                            className={cn(
                                "relative h-10 shrink-0 px-2.5 inline-flex items-center text-[12.5px] whitespace-nowrap",
                                shownTab === t ? "text-slate-900 font-medium" : "text-slate-500 hover:text-slate-800",
                            )}
                        >
                            {TAB_LABELS[t]}
                            {shownTab === t && <span className="absolute left-2 right-2 -bottom-px h-0.5 rounded-full bg-sky-600" />}
                        </button>
                    ))}
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

            <div className="bg-slate-100 p-3">
                <Stage width={view.width} height={view.height} framed={device === "phone"}>
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
            </div>
            <p className="border-t border-slate-200 px-3 py-2 text-[11.5px] text-slate-500">{CAPTIONS[shownTab]}</p>
        </div>
    );
}

// Draws its children at a fixed page size and scales the result to the room there is.
function Stage({ width, height, framed, children }: { width: number; height: number; framed: boolean; children: React.ReactNode }) {
    const ref = React.useRef<HTMLDivElement>(null);
    const [room, setRoom] = React.useState(0);
    React.useLayoutEffect(() => {
        const el = ref.current;
        if (!el) return;
        const measure = () => setRoom(el.clientWidth);
        measure();
        const ro = new ResizeObserver(measure);
        ro.observe(el);
        return () => ro.disconnect();
    }, []);

    // A phone is fitted to a height, so it never grows taller than the desktop view beside the editor.
    const scale = room ? (framed ? Math.min(room / width, (room * 0.92) / height) : room / width) : 0;
    return (
        <div ref={ref} className="flex justify-center">
            <div
                className={cn(
                    "relative shrink-0 overflow-hidden bg-white shadow-sm ring-1 ring-slate-900/10",
                    framed ? "rounded-[14px]" : "rounded-md",
                )}
                style={{ width: width * scale, height: height * scale }}
            >
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
