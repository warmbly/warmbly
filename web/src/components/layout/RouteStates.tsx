// Loading and error states for routes; the boot state is the dashboard's own frame drawn in outline, so nothing moves when it fills in.

import { useRouter, type ErrorComponentProps } from "@tanstack/react-router";
import { useEffect, type CSSProperties } from "react";
import { Logo } from "@/components/svg";
import { cn } from "@/lib/utils";
import { captureException } from "@/lib/observability";
import { canReloadForStaleChunk, isModuleLoadError, reloadForStaleChunk } from "@/lib/moduleLoadError";
import { useAppStore } from "@/stores";
import { SkyChrome } from "./SkyChrome";
import { BoundaryFallback } from "./ErrorBoundary";

// Fixed widths, so the outline reads as real labels and never reshuffles between renders.
const LABEL_WIDTHS = [62, 78, 54, 70, 46, 84, 58, 66, 50, 74, 60, 42, 68, 56, 80, 48];
// Rows per sidebar section, as AppNav draws them: Inbox, then Email, CRM and Resources.
const NAV_SECTIONS = [1, 7, 4, 5];
const TABLE_ROWS = 9;

const rise = (i: number): CSSProperties => ({ animationDelay: `${60 + i * 35}ms` });
const RISE = "animate-in fade-in slide-in-from-bottom-1 duration-500 fill-mode-both motion-reduce:animate-none";

function Bar({ className, style }: { className?: string; style?: CSSProperties }) {
    return <div className={cn("skeleton-sync rounded", className)} style={style} />;
}

// A page still loading: a list page in outline, its rows settling in one after another.
export function RouteFallback() {
    return (
        <div className="px-5 pt-5 pb-8" role="status" aria-label="Loading">
            <div className={cn("flex items-start justify-between gap-4", RISE)} style={rise(0)}>
                <div className="space-y-2.5">
                    <Bar className="h-6 w-44 rounded-md" />
                    <Bar className="h-3 w-72 max-w-[60vw]" />
                </div>
                <div className="hidden sm:flex items-center gap-2">
                    <Bar className="h-7 w-24 rounded-md" />
                    <Bar className="h-7 w-28 rounded-md" />
                </div>
            </div>
            <div className={cn("mt-6 flex items-center gap-2", RISE)} style={rise(1)}>
                <Bar className="h-7 w-64 max-w-[50vw] rounded-md" />
                <Bar className="h-7 w-20 rounded-md" />
                <Bar className="h-7 w-20 rounded-md" />
            </div>
            <div className={cn("mt-4 rounded-lg border border-slate-200 overflow-hidden", RISE)} style={rise(2)}>
                <div className="h-9 px-4 flex items-center gap-4 bg-slate-50/80">
                    <Bar className="size-3.5 rounded-[4px]" />
                    <Bar className="h-2.5 w-24" />
                    <Bar className="h-2.5 w-16 ml-auto" />
                    <Bar className="hidden md:block h-2.5 w-14" />
                    <Bar className="hidden md:block h-2.5 w-12" />
                </div>
                {Array.from({ length: TABLE_ROWS }, (_, i) => (
                    <div key={i} className={cn("h-14 px-4 flex items-center gap-4 border-t border-slate-100", RISE)} style={rise(3 + i)}>
                        <Bar className="size-3.5 rounded-[4px]" />
                        <Bar className="size-7 rounded-full" />
                        <div className="space-y-2 min-w-0 flex-1">
                            <Bar className="h-3" style={{ width: `${LABEL_WIDTHS[i] * 2.4}px`, maxWidth: "100%" }} />
                            <Bar className="h-2.5" style={{ width: `${LABEL_WIDTHS[i + 4] * 1.6}px`, maxWidth: "80%" }} />
                        </div>
                        <Bar className="h-5 w-16 rounded-full" />
                        <Bar className="hidden md:block h-2.5 w-14" />
                        <Bar className="hidden md:block h-2.5 w-12" />
                    </div>
                ))}
            </div>
        </div>
    );
}

// Pages outside the dashboard keep the background until they can paint.
export function BlankPending() {
    return null;
}

export function RouteError({ error, reset, info }: ErrorComponentProps) {
    const router = useRouter();
    useEffect(() => {
        if (reloadForStaleChunk(error)) return;
        captureException(error);
    }, [error]);
    // A chunk a deploy removed: the effect above reloads into the new build.
    if (isModuleLoadError(error) && canReloadForStaleChunk()) return null;
    return (
        <BoundaryFallback
            error={error as Error}
            info={info ? { componentStack: info.componentStack } : null}
            reset={() => {
                reset();
                void router.invalidate();
            }}
        />
    );
}

function NavOutline({ collapsed }: { collapsed: boolean }) {
    let row = 0;
    return (
        <div className={cn("hidden md:flex flex-col shrink-0 pt-1", collapsed ? "w-14" : "w-64")}>
            {NAV_SECTIONS.map((count, s) => (
                <div key={s} className={cn(s > 0 && "mt-4")}>
                    {s > 0 && !collapsed && <Bar className="mx-3 ml-[21px] mb-2 h-2 w-14" />}
                    <div className="space-y-px">
                        {Array.from({ length: count }, () => {
                            const i = row++;
                            return (
                                <div key={i} className={cn("mx-3 h-7 flex items-center gap-2.5 px-[9px]", RISE)} style={rise(i)}>
                                    <Bar className="size-3.5 shrink-0 rounded-[4px]" />
                                    {!collapsed && <Bar className="h-2.5" style={{ width: LABEL_WIDTHS[i % LABEL_WIDTHS.length] }} />}
                                </div>
                            );
                        })}
                    </div>
                </div>
            ))}
            <div className="mt-auto border-t border-slate-200/60 py-2">
                <div className="mx-3 h-7 flex items-center gap-2.5 px-[9px]">
                    <Bar className="size-3.5 shrink-0 rounded-[4px]" />
                    {!collapsed && <Bar className="h-2.5 w-14" />}
                </div>
            </div>
            <div className="border-t border-slate-200/60 h-14 flex items-center gap-2.5 px-5">
                <Bar className="size-7 shrink-0 rounded-full" />
                {!collapsed && (
                    <div className="space-y-1.5">
                        <Bar className="h-2.5 w-24" />
                        <Bar className="h-2 w-32" />
                    </div>
                )}
            </div>
        </div>
    );
}

// The dashboard frame in outline, shown while the session boots: the real header, sidebar and a page, filled in place.
export function ShellSkeleton() {
    const collapsed = useAppStore((s) => s.navCollapsed);
    return (
        <div className="fixed inset-0 flex flex-col" role="status" aria-label="Loading">
            <SkyChrome />
            <div className="relative z-10 flex flex-col h-full">
                <div className="h-14 flex items-center shrink-0">
                    <div className={cn("h-full flex items-center gap-2.5 shrink-0 pl-4 pr-3", collapsed ? "md:w-14 md:px-0 md:justify-center" : "md:w-64 md:px-5")}>
                        <Logo className="w-7 text-slate-900" />
                        <span
                            style={{ fontFamily: "var(--font-display)" }}
                            className={cn("wb-title font-extrabold text-[15.5px] tracking-tight text-slate-900", collapsed ? "hidden" : "hidden md:inline")}
                        >
                            Warmbly
                        </span>
                    </div>
                    <div className="flex items-center gap-2 min-w-0 flex-1">
                        <Bar className="size-5 rounded-md" />
                        <Bar className="h-3 w-24" />
                        <Bar className="hidden md:block h-3 w-20 ml-3" />
                    </div>
                    <div className="flex items-center gap-2 px-4 shrink-0">
                        <Bar className="hidden lg:block h-6 w-20 rounded-full" />
                        <Bar className="hidden sm:block h-7 w-24 rounded-md" />
                        <Bar className="size-7 rounded-md" />
                    </div>
                </div>
                <div className="flex-1 flex min-h-0">
                    <NavOutline collapsed={collapsed} />
                    <main className="wb-panel relative flex-1 min-w-0 bg-white overflow-hidden border-t border-slate-200/70 md:rounded-tl-2xl md:border-l">
                        <div aria-hidden className="absolute inset-x-0 top-0 h-0.5 overflow-hidden">
                            <div className="h-full w-1/3 bg-gradient-to-r from-transparent via-sky-500 to-transparent progress-sweep" />
                        </div>
                        <RouteFallback />
                    </main>
                </div>
            </div>
        </div>
    );
}
