import { describe, expect, it } from "vitest";
import buildError from "./buildError";

describe("buildError", () => {
    it("shows a portal session's refusal as its own sentence", () => {
        const message = "You signed in on an agency portal, which reaches only that agency's client workspaces. Sign in at https://app.warmbly.test to do this.";
        expect(buildError({ error: "Forbidden", code: "portal_session_restricted", status: 403, message })).toBe(message);
    });
    it("keeps the status and request id on any other error", () => {
        expect(buildError({ error: "Forbidden", code: "forbidden", status: 403, message: "No.", request_id: "r1" })).toBe("403 Forbidden: No. (r1)");
    });
});
