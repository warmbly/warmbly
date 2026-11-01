import { beforeEach, describe, expect, it, vi } from "vitest";
import { CanceledError } from "axios";
import Client from "./Client";
import Request from "./Request";
import getToken from "@/lib/helper/getToken";
import refreshTokenFn from "./auth/refreshToken";
import { AuthError } from "@/lib/errors/auth";
import { SESSION_ENDED_EVENT } from "@/lib/auth";
import { queryCompanyEnrich, enrichCompanyEnrichContact } from "./app/integrations/companyenrich";

vi.mock("./auth/refreshToken", () => ({
    default: vi.fn(),
}));

// Counted rather than asserted on localStorage alone: ending a session has to
// say so, or nothing navigates and the app sits on a page it cannot load.
let endedSessions = 0;
window.addEventListener(SESSION_ENDED_EVENT, () => {
    endedSessions += 1;
});

vi.mock("./Client", () => ({
    default: {
        request: vi.fn(),
    },
}));

vi.mock("@/lib/helper/getToken", () => ({
    default: vi.fn(),
}));

describe("Request", () => {
    beforeEach(() => {
        vi.clearAllMocks();
        endedSessions = 0;
        vi.mocked(getToken).mockReturnValue({
            access_token: "access-token",
            access_token_expires_at: new Date(Date.now() + 60_000),
            refresh_token: "refresh-token",
            refresh_token_expires_at: new Date(Date.now() + 120_000),
        });
    });

    it("does not replay paid requests after authentication failure", async () => {
        const error = { status: 401, message: "Request refused" };
        vi.mocked(Client.request).mockRejectedValue(error);
        await expect(queryCompanyEnrich("id", { operation: "enrich_company", query: { domain: "example.com" } })).rejects.toBe(error);
        expect(Client.request).toHaveBeenCalledTimes(1);
    });

    it("does not replay paid requests through a reauthentication prompt", async () => {
        const error = { status: 403, code: "reauth_required", message: "Request refused" };
        vi.mocked(Client.request).mockRejectedValue(error);
        const prompt = vi.fn();
        window.addEventListener("reauth-required", prompt);
        try {
            await expect(enrichCompanyEnrichContact("id", { contact_id: "contact-id", kind: "person", overwrite: false })).rejects.toBe(error);
            expect(Client.request).toHaveBeenCalledTimes(1);
            expect(prompt).not.toHaveBeenCalled();
        } finally { window.removeEventListener("reauth-required", prompt); }
    });

    it("preserves opaque provider JSON including date-shaped strings and exact IDs", async () => {
        const payload = { data: { id: "9223372036854775807", created_at: "2026-09-17T12:34:56.000Z", nested: [null] }, credits_consumed: 0 };
        vi.mocked(Client.request).mockResolvedValue({ data: payload });
        expect(await queryCompanyEnrich("id", { operation: "get_person", query: { id: "9223372036854775807" } })).toBe(payload);
        expect(payload.data.created_at).toBe("2026-09-17T12:34:56.000Z");
    });

    it("revives saved contact dates without changing the enrichment payload", async () => {
        const timestamp = "2026-09-17T12:34:56.000Z";
        const provider = { created_at: timestamp, id: "9223372036854775807" };
        vi.mocked(Client.request).mockResolvedValue({ data: { contact: { created_at: timestamp, custom_fields: { date_text: timestamp } }, data: provider, updated_fields: [] } });
        const result = await enrichCompanyEnrichContact("id", { contact_id: "contact-id", kind: "person", overwrite: false });
        expect(result.contact.created_at).toEqual(new Date(timestamp));
        expect(result.contact.custom_fields.date_text).toBe(timestamp);
        expect(result.data).toBe(provider);
    });

    it("preserves blob responses for browser downloads", async () => {
        const blob = new Blob(["email\nlead@example.com\n"], { type: "text/csv" });
        vi.mocked(Client.request).mockResolvedValue({ data: blob });

        const result = await Request<Blob>({
            method: "POST",
            url: "/contacts/export",
            authorization: true,
            responseType: "blob",
        });

        expect(result).toBe(blob);
        expect(result).toBeInstanceOf(Blob);
    });

    it("continues reviving timestamps in JSON responses", async () => {
        vi.mocked(Client.request).mockResolvedValue({
            data: {
                created_at: "2026-09-17T12:34:56.000Z",
                nested: [{ updated_at: "2026-09-18T01:02:03.000Z" }],
                label: "2026-09-17",
            },
        });

        const result = await Request<{
            created_at: Date;
            nested: Array<{ updated_at: Date }>;
            label: string;
        }>({
            method: "GET",
            url: "/contacts/example",
            authorization: true,
        });

        expect(result.created_at).toEqual(new Date("2026-09-17T12:34:56.000Z"));
        expect(result.nested[0]?.updated_at).toEqual(new Date("2026-09-18T01:02:03.000Z"));
        expect(result.label).toBe("2026-09-17");
    });

    it("hands the caller's signal to axios and rethrows a cancellation untouched", async () => {
        const controller = new AbortController();
        const cancelled = new CanceledError();
        vi.mocked(Client.request).mockRejectedValue(cancelled);

        await expect(
            Request({ method: "GET", url: "/unibox/thread", authorization: true, signal: controller.signal }),
        ).rejects.toBe(cancelled);

        expect(Client.request).toHaveBeenCalledTimes(1);
        expect(vi.mocked(Client.request).mock.calls[0][0].signal).toBe(controller.signal);
        expect(endedSessions).toBe(0);
    });

    // A tab whose token went away in another tab keeps every page it rendered
    // mounted and every poll on them running. Each poll lands here, and until
    // this said so the app stayed on a page it could not authenticate and threw
    // one unhandled AuthError a minute for as long as the tab was open.
    it("ends the session when there is no token at all", async () => {
        vi.mocked(getToken).mockReturnValue(null);

        await expect(
            Request({ method: "GET", url: "/emails", authorization: true }),
        ).rejects.toBeInstanceOf(AuthError);

        expect(endedSessions).toBe(1);
        expect(Client.request).not.toHaveBeenCalled();
    });

    // An unauthorized call is how the login and registration pages talk to the
    // API, and none of them has a session to end.
    it("leaves an unauthorized call alone when there is no token", async () => {
        vi.mocked(getToken).mockReturnValue(null);
        vi.mocked(Client.request).mockResolvedValue({ data: { ok: true } });

        await expect(Request({ method: "POST", url: "/auth/login" })).resolves.toEqual({ ok: true });

        expect(endedSessions).toBe(0);
    });

    // The database ran out of connection slots, /auth/refresh answered 500 for
    // about a minute, and everyone whose access token expired in that minute
    // was signed out and could not get back in: the refresh token the server
    // had never refused was deleted along with the session.
    describe("when the access token has expired", () => {
        const expiredAccess = () => {
            vi.mocked(getToken).mockReturnValue({
                access_token: "access-token",
                access_token_expires_at: new Date(Date.now() - 60_000),
                refresh_token: "refresh-token",
                refresh_token_expires_at: new Date(Date.now() + 120_000),
            });
        };

        beforeEach(() => {
            expiredAccess();
        });

        it("keeps the session when the refresh only failed to get an answer", async () => {
            vi.mocked(refreshTokenFn).mockRejectedValue({
                error: "Internal Server Error",
                message: "Something went wrong.",
                status: 500,
            });

            await expect(
                Request({ method: "GET", url: "/emails", authorization: true }),
            ).rejects.not.toBeInstanceOf(AuthError);

            expect(endedSessions).toBe(0);
        });

        it("keeps the session when the refresh never reached the server", async () => {
            vi.mocked(refreshTokenFn).mockRejectedValue({
                error: "Network Error",
                message: "Please check your connection.",
            });

            await expect(
                Request({ method: "GET", url: "/emails", authorization: true }),
            ).rejects.not.toBeInstanceOf(AuthError);

            expect(endedSessions).toBe(0);
        });

        it("ends the session when the server refuses the refresh token", async () => {
            vi.mocked(refreshTokenFn).mockRejectedValue({
                error: "Unauthorized",
                message: "Your session is invalid or expired.",
                status: 401,
            });

            await expect(
                Request({ method: "GET", url: "/emails", authorization: true }),
            ).rejects.toBeInstanceOf(AuthError);

            expect(endedSessions).toBe(1);
        });
    });
});
