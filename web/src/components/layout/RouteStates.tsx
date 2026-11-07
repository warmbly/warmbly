// Loading and error states for routes; the boot state is the dashboard's own empty frame, so nothing moves.

import { useRouter, type ErrorComponentProps } from "@tanstack/react-router";
import { useEffect } from "react";
import { Logo } from "@/components/svg";
import { cn } from "@/lib/utils";
import { captureException } from "@/lib/observability";
import { canReloadForStaleChunk, isModuleLoadError, reloadForStaleChunk } from "@/lib/moduleLoadError";
import { useAppStore } from "@/stores";
import { SkyChrome } from "./SkyChrome";
import { BoundaryFallback } from "./ErrorBoundary";

// A page still loading: the same hairline blocks the pages draw themselves.
export function RouteFallback() {
    return (
        <div className="px-5 pt-5 space-y-4 animate-in fade-in duration-200" role="status" aria-label="Loading">
            <div className="h-6 w-56 bg-slate-100 rounded-md animate-pulse" />
            <div className="h-3 w-40 bg-slate-100 rounded animate-pulse" />
            <div className="h-56 bg-slate-100 rounded-md animate-pulse" />
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

// The dashboard frame with nothing in it yet, shown while the session boots.
export function ShellSkeleton() {
    const collapsed = useAppStore((s) => s.navCollapsed);
    return (
        <div className="fixed inset-0 flex flex-col" role="status" aria-label="Loading">
            <SkyChrome />
            <div className="relative z-10 flex flex-col h-full">
                <div className="h-14 flex items-center shrink-0 px-4">
                    <Logo className="w-7 text-slate-900" />
                </div>
                <div className="flex-1 flex min-h-0">
                    <div className={cn("hidden md:flex flex-col gap-1.5 shrink-0 px-3 pt-1", collapsed ? "w-14" : "w-64")}>
                        {Array.from({ length: 7 }, (_, i) => (
                            <div key={i} className={cn("h-7 rounded-md bg-slate-200/50 animate-pulse", collapsed && "w-8")} />
                        ))}
                    </div>
                    <main className="wb-panel flex-1 min-w-0 bg-white overflow-hidden border-t border-slate-200/70 md:rounded-tl-2xl md:border-l">
                        <RouteFallback />
                    </main>
                </div>
            </div>
        </div>
    );
}
