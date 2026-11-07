import { beforeEach, describe, expect, it, vi } from "vitest";

const captured = vi.hoisted(() => [] as Array<{ error: unknown; properties?: Record<string, unknown> }>);
vi.mock("@/lib/observability", () => ({
    captureException: (error: unknown, properties?: Record<string, unknown>) => captured.push({ error, properties }),
    noteStep: () => {},
}));

import { APIError, ReauthCancelledError, SessionExpiredError } from "@/lib/api/client";
import { reportFailure } from "./reportFailure";

function apiError(status: number): APIError {
    const e = new APIError("invalid query parameters", status, { code: "bad_request", request_id: "req-1" });
    e.method = "GET";
    e.path = "/admin/users";
    return e;
}

describe("reportFailure", () => {
    beforeEach(() => {
        captured.length = 0;
    });

    it("reports a 400 on a query with the call that failed", () => {
        reportFailure(apiError(400), "query");
        expect(captured).toHaveLength(1);
        expect(captured[0].properties).toEqual({
            kind: "query",
            status: 400,
            method: "GET",
            path: "/admin/users",
            code: "bad_request",
            request_id: "req-1",
        });
    });

    it("reports a 5xx mutation but not a 4xx one", () => {
        reportFailure(apiError(422), "mutation");
        reportFailure(apiError(500), "mutation");
        expect(captured.map((c) => (c.error as APIError).status)).toEqual([500]);
    });

    it("skips expected answers, the network and a lapsed session", () => {
        for (const status of [0, 401, 403, 404, 429]) reportFailure(apiError(status), "query");
        reportFailure(new SessionExpiredError(), "query");
        expect(captured).toHaveLength(0);
    });

    it("skips a confirmation the operator cancelled", () => {
        reportFailure(new ReauthCancelledError(), "mutation");
        expect(captured).toHaveLength(0);
    });

    it("reports an error thrown outside the API client", () => {
        reportFailure(new TypeError("x is undefined"), "query");
        expect(captured).toHaveLength(1);
    });
});
