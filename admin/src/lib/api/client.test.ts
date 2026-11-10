import { beforeEach, describe, expect, it, vi } from "vitest";

const http = vi.hoisted(() => ({ request: vi.fn() }));
vi.mock("axios", () => ({
    default: {
        create: () => http,
        isAxiosError: (e: unknown) => !!(e as { isAxiosError?: boolean } | null)?.isAxiosError,
        post: vi.fn(),
    },
}));
vi.mock("@/lib/env", () => ({ API_URL: "http://localhost:8080" }));
vi.mock("@/lib/observability", () => ({ noteStep: () => {} }));
vi.mock("@/lib/auth/storage", () => ({
    getToken: () => ({ access_token: "token", access_token_expires_at: "2100-01-01T00:00:00Z" }),
    isExpired: () => false,
    setToken: () => {},
    clearToken: () => {},
}));

import { APIError, Request } from "@/lib/api/client";

function refusal(status: number, data: Record<string, string>) {
    return { isAxiosError: true, message: `Request failed with status code ${status}`, response: { status, data } };
}

describe("a call that needs a fresh sign-in", () => {
    beforeEach(() => {
        http.request.mockReset();
    });

    it("answers a refused retry with the server's own message", async () => {
        http.request
            .mockRejectedValueOnce(refusal(403, { code: "reauth_required" }))
            .mockRejectedValueOnce(refusal(403, { code: "agency_client_owner_not_staff", message: "The owner is not on the agency's staff." }));
        const confirmed = (e: Event) => (e as CustomEvent<{ resolve: () => void }>).detail.resolve();
        window.addEventListener("reauth-required", confirmed);
        const call = Request({ method: "POST", url: "/admin/agencies/a/clients", authorization: true, headers: { "Idempotency-Key": "key-1" } });
        await expect(call).rejects.toBeInstanceOf(APIError);
        await expect(call).rejects.toMatchObject({
            message: "The owner is not on the agency's staff.",
            status: 403,
            code: "agency_client_owner_not_staff",
        });
        window.removeEventListener("reauth-required", confirmed);
        // The retry is the same request, so a keyed write is not applied twice.
        expect(http.request).toHaveBeenCalledTimes(2);
        expect(http.request.mock.calls[1][0].headers["Idempotency-Key"]).toBe("key-1");
    });
});
