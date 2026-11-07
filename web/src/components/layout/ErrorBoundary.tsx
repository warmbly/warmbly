// Route-level error boundary.
//
// Without this, an unhandled render error in any page unmounts the entire
// subtree to the next React boundary (which is "none" in this app), so the
// content panel goes silent-white with no signal about what broke. This
// boundary catches it, prints the actual error inside the panel using the
// same brae-density chrome as the rest of the app, and offers a retry.
//
// Wrap each route element with <RouteBoundary>...</RouteBoundary> or use
// the <withBoundary> helper to opt a page in.

import React from "react";
import { useLocation, useRouter } from "@tanstack/react-router";
import { AlertTriangleIcon, RefreshCcwIcon } from "lucide-react";
import { captureException } from "@/lib/observability";
import { SENTRY_RELEASE } from "@/lib/information";
import { canReloadForStaleChunk, isModuleLoadError, reloadForStaleChunk } from "@/lib/moduleLoadError";

interface State {
    error: Error | null;
    info: React.ErrorInfo | null;
    /** Last `resetKey` seen, so a change clears the error without a remount. */
    seenResetKey?: string;
}

interface BoundaryProps {
    children: React.ReactNode;
    onReset?: () => void;
    /** Change this to drop a caught error and re-render the children. */
    resetKey?: string;
}

export class ErrorBoundary extends React.Component<BoundaryProps, State> {
    state: State = { error: null, info: null };

    static getDerivedStateFromError(error: Error): Partial<State> {
        return { error };
    }

    // Clearing the error here rather than remounting the boundary is what lets
    // a healthy page keep its state across a URL change.
    static getDerivedStateFromProps(props: BoundaryProps, state: State): Partial<State> | null {
        if (state.seenResetKey === props.resetKey) return null;
        return { seenResetKey: props.resetKey, error: null, info: null };
    }

    componentDidCatch(error: Error, info: React.ErrorInfo) {
        if (reloadForStaleChunk(error)) return;
        this.setState({ info });
        // Through lib/observability, so the error reaches whichever backend
        // the deployment configured. Reading a global off `window` found the
        // SDK only when it had put itself there, which a bundled one does not.
        captureException(error);
        console.error("[ErrorBoundary]", error, info?.componentStack);
    }

    reset = () => {
        this.setState({ error: null, info: null });
        this.props.onReset?.();
    };

    render() {
        if (!this.state.error) return this.props.children;
        if (isModuleLoadError(this.state.error) && canReloadForStaleChunk()) return null;
        return <BoundaryFallback error={this.state.error} info={this.state.info} reset={this.reset} />;
    }
}

export function BoundaryFallback({ error, info, reset }: { error: Error; info: Pick<React.ErrorInfo, "componentStack"> | null; reset: () => void }) {
    const router = useRouter();
    const reload = isModuleLoadError(error);
    return (
        <div className="flex flex-col min-h-full bg-white">
            <div className="min-h-12 md:h-12 px-5 py-1.5 md:py-0 border-b border-slate-200 flex flex-wrap md:flex-nowrap items-center gap-3 gap-y-1.5 shrink-0 bg-white">
                <span className="text-[10px] uppercase tracking-[0.14em] text-red-500 font-medium">
                    Page error
                </span>
                <div className="h-4 w-px bg-slate-200" />
                <span className="text-[12.5px] text-slate-600 truncate">
                    {error.message || "Something broke while rendering this page"}
                </span>
                <div className="ml-auto flex items-center gap-1.5">
                    <button
                        onClick={() => router.history.back()}
                        className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-slate-700 hover:text-slate-900 text-[12px] font-medium transition-colors"
                    >
                        Back
                    </button>
                    <button
                        onClick={() => reload ? window.location.reload() : reset()}
                        className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
                    >
                        <RefreshCcwIcon className="w-3 h-3" />
                        {reload ? "Reload" : "Retry"}
                    </button>
                </div>
            </div>

            <div className="flex-1 min-h-0 overflow-auto px-5 py-6">
                <div className="max-w-3xl">
                    <div className="flex items-center gap-2 mb-3">
                        <AlertTriangleIcon className="w-3.5 h-3.5 text-red-500 shrink-0" />
                        <span className="text-[12.5px] font-semibold text-slate-900">
                            {error.name || "Error"}
                        </span>
                    </div>
                    <p className="text-[12px] text-slate-700 mb-4 leading-relaxed">
                        {error.message || "No message provided."}
                    </p>
                    {(error.stack || info?.componentStack) && (
                        <details className="border border-slate-200 rounded-md bg-slate-50 overflow-hidden">
                            <summary className="px-3 py-2 text-[11px] font-medium text-slate-700 cursor-pointer hover:bg-slate-100 transition-colors">
                                Stack
                            </summary>
                            <pre className="px-3 py-3 text-[10.5px] font-mono text-slate-700 leading-relaxed overflow-x-auto whitespace-pre-wrap border-t border-slate-200">
                                {SENTRY_RELEASE ? `Build: ${SENTRY_RELEASE}\n\n` : ""}
                                {error.stack || ""}
                                {info?.componentStack ? `\n\nComponent stack:${info.componentStack}` : ""}
                            </pre>
                        </details>
                    )}
                </div>
            </div>
        </div>
    );
}

/**
 * RouteBoundary resets the boundary on
 * pathname change so navigating away from a broken page recovers
 * automatically.
 */
export function RouteBoundary({ children }: { children: React.ReactNode }) {
    const { pathname } = useLocation();
    // resetKey, not key: a `key` here would tear down and re-create the whole
    // routed subtree on every URL change, which throws away the scroll offset
    // of any list whose page encodes in-page state in the path (issue #396).
    // Mounting the next page cleanly is the Suspense boundary's job in
    // AppShell, which keys off the route identity instead.
    return <ErrorBoundary resetKey={pathname}>{children}</ErrorBoundary>;
}
