import { describe, expect, it, vi } from "vitest";
import { describeAdminDevice, decideAdminDevice } from "./device";
const request = vi.hoisted(() => vi.fn());
vi.mock("@/lib/api/client", () => ({ Request: request }));

describe("device API consent contract", () => {
    it("uses authorized describe without any automatic grant or poll request", async () => {
        request.mockResolvedValueOnce({ request: { status: "pending" } });
        await describeAdminDevice("ABCD-EFGH");
        expect(request).toHaveBeenLastCalledWith({ method: "POST", url: "/admin/auth/device/describe", authorization: true, data: { user_code: "ABCD-EFGH" } });
    });
    it("disables the usual automatic retry after fresh-auth so consent is explicit twice", async () => {
        await decideAdminDevice("ABCD-EFGH", "fixture-consent", "denied");
        expect(request).toHaveBeenLastCalledWith({ method: "POST", url: "/admin/auth/device/decide", authorization: true, skipReauthPrompt: true, data: { user_code: "ABCD-EFGH", consent_token: "fixture-consent", decision: "denied" } });
    });
});
