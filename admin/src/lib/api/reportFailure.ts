// Reports a failed query or mutation. react-query keeps these as error state
// for the page to render, so without this nothing reaches PostHog or Sentry.

import { APIError, ReauthCancelledError, SessionExpiredError } from "@/lib/api/client";
import { captureException, type StepProperties } from "@/lib/observability";

// Answers that are the expected outcome of a request, not a defect: a signed-out
// or under-permitted operator, a record deleted since the list loaded, a limiter.
const EXPECTED = new Set([401, 403, 404, 429]);

export function shouldReport(error: unknown, kind: "query" | "mutation"): boolean {
    if (error instanceof SessionExpiredError || error instanceof ReauthCancelledError) return false;
    if (!(error instanceof APIError)) return true;
    // Status 0 is the network, not the panel.
    if (error.status === 0 || EXPECTED.has(error.status)) return false;
    // A 4xx on a mutation is usually the operator's input, which the form shows.
    return kind === "query" || error.status >= 500;
}

export function reportFailure(error: unknown, kind: "query" | "mutation"): void {
    if (!shouldReport(error, kind)) return;
    const properties: StepProperties = { kind };
    if (error instanceof APIError) {
        properties.status = error.status;
        if (error.method) properties.method = error.method;
        if (error.path) properties.path = error.path;
        if (error.code) properties.code = error.code;
        if (error.requestId) properties.request_id = error.requestId;
    }
    captureException(error, properties);
}
