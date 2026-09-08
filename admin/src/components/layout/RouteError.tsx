// Error boundary for the authenticated shell. A page that throws during
// render (or a loader that rejects) lands here instead of a blank screen:
// the message, the backend code and request id when it was an API error,
// and a way to retry. Reported once to Sentry when a DSN is configured.

import { useEffect, useRef } from "react";
import { isRouteErrorResponse, Link, useRouteError } from "react-router-dom";
import { AlertTriangle, House, RotateCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { APIError, SessionExpiredError } from "@/lib/api/client";
import { captureException } from "@/lib/observability";

export function RouteError() {
    const error = useRouteError();
    const reported = useRef(false);

    useEffect(() => {
        if (reported.current) return;
        reported.current = true;
        captureException(error);
    }, [error]);

    const api = error instanceof APIError ? error : null;
    let title = "This page hit an error";
    let message: string;
    if (error instanceof SessionExpiredError) {
        title = "Your session expired";
        message = "Sign in again to keep working.";
    } else if (isRouteErrorResponse(error)) {
        title = `${error.status} ${error.statusText}`.trim();
        message = typeof error.data === "string" ? error.data : "The router could not render this route.";
    } else if (error instanceof Error) {
        message = error.message || "An unexpected error occurred.";
    } else {
        message = "An unexpected error occurred.";
    }

    const meta = [
        api?.status ? `HTTP ${api.status}` : null,
        api?.code ? `code ${api.code}` : null,
        api?.requestId ? `request ${api.requestId}` : null,
    ].filter(Boolean) as string[];

    return (
        <div className="mx-auto mt-12 max-w-lg rounded-lg border border-border bg-card p-6">
            <div className="flex items-start gap-3">
                <span className="mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-full bg-[color-mix(in_oklab,var(--admin-danger)_12%,transparent)] text-[var(--admin-danger)]">
                    <AlertTriangle className="size-4" />
                </span>
                <div className="min-w-0 flex-1">
                    <div className="text-base font-semibold text-foreground">{title}</div>
                    <p className="mt-1 text-sm text-muted-foreground break-words">{message}</p>
                    {meta.length > 0 && (
                        <div className="mt-2 flex flex-wrap gap-x-2 gap-y-1 font-mono text-[11px] text-muted-foreground select-text">
                            {meta.map((m, i) => (
                                <span key={m}>
                                    {i > 0 && <span className="mr-2 opacity-50">·</span>}
                                    {m}
                                </span>
                            ))}
                        </div>
                    )}
                    <div className="mt-4 flex flex-wrap gap-2">
                        <Button size="sm" onClick={() => window.location.reload()}>
                            <RotateCw className="size-3.5" />
                            Try again
                        </Button>
                        <Button size="sm" variant="outline" asChild>
                            <Link to="/">
                                <House className="size-3.5" />
                                Back to overview
                            </Link>
                        </Button>
                    </div>
                </div>
            </div>
        </div>
    );
}
