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
import { useNavigate, useLocation } from "react-router-dom";
import { AlertTriangleIcon, RefreshCcwIcon } from "lucide-react";

interface State {
    error: Error | null;
    info: React.ErrorInfo | null;
}

export class ErrorBoundary extends React.Component<
    { children: React.ReactNode; onReset?: () => void },
    State
> {
    state: State = { error: null, info: null };

    static getDerivedStateFromError(error: Error): Partial<State> {
        return { error };
    }

    componentDidCatch(error: Error, info: React.ErrorInfo) {
        this.setState({ info });
        if (typeof window !== "undefined" && (window as unknown as { Sentry?: { captureException: (e: Error) => void } }).Sentry) {
            (window as unknown as { Sentry?: { captureException: (e: Error) => void } }).Sentry?.captureException(error);
        }
        console.error("[ErrorBoundary]", error, info?.componentStack);
    }

    reset = () => {
        this.setState({ error: null, info: null });
        this.props.onReset?.();
    };

    render() {
        if (!this.state.error) return this.props.children;
        return <BoundaryFallback error={this.state.error} info={this.state.info} reset={this.reset} />;
    }
}

function BoundaryFallback({ error, info, reset }: { error: Error; info: React.ErrorInfo | null; reset: () => void }) {
    const navigate = useNavigate();
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
                        onClick={() => navigate(-1)}
                        className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-slate-700 hover:text-slate-900 text-[12px] font-medium transition-colors"
                    >
                        Back
                    </button>
                    <button
                        onClick={reset}
                        className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
                    >
                        <RefreshCcwIcon className="w-3 h-3" />
                        Retry
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
 * RouteBoundary — react-router compatible: resets the boundary on
 * pathname change so navigating away from a broken page recovers
 * automatically.
 */
export function RouteBoundary({ children }: { children: React.ReactNode }) {
    const { pathname } = useLocation();
    // Key forces a fresh ErrorBoundary instance on every route change, which
    // both clears stale errors and lets the new page mount cleanly.
    return <ErrorBoundary key={pathname}>{children}</ErrorBoundary>;
}
