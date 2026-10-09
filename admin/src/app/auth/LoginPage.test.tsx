import { act, type PropsWithChildren } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import LoginPage from "./LoginPage";

const state = vi.hoisted(() => ({
    login: vi.fn(), confirm: vi.fn(), verify: vi.fn(), saveToken: vi.fn(),
    onToken: (_value: string): Promise<void> => Promise.resolve(),
    onCode: (_value: string): void => {},
}));
vi.mock("@/lib/api/client/auth", () => ({ getAuthConfig: () => Promise.resolve({ captcha: false }), login: state.login, loginConfirm: state.confirm, verifyTwoFA: state.verify }));
vi.mock("@/lib/auth/storage", () => ({ setToken: state.saveToken }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("motion/react", () => ({ AnimatePresence: ({ children }: PropsWithChildren) => children, motion: { div: ({ children }: PropsWithChildren) => <div>{children}</div> } }));
vi.mock("@/components/captcha/TurnstileModal", () => ({ TurnstileModal: ({ onToken }: { onToken: typeof state.onToken }) => { state.onToken = onToken; return null; } }));
vi.mock("@/components/ui/input-otp", () => ({ InputOTP: ({ onChange }: { onChange: typeof state.onCode }) => { state.onCode = onChange; return null; }, InputOTPGroup: () => null, InputOTPSlot: () => null }));

const token = { access_token: "fixture-access", refresh_token: "fixture-refresh", access_token_expires_at: "2099-01-01T00:00:00Z", refresh_token_expires_at: "2099-01-01T00:00:00Z" };
let root: Root;
let client: QueryClient;
let clear: ReturnType<typeof vi.spyOn>;

beforeEach(async () => {
    vi.clearAllMocks();
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
    client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    client.setQueryData(["me"], { id: "previous-admin", admin_permissions: 0xffff });
    client.setQueryData(["system", "mail-status"], { private: "previous-session" });
    clear = vi.spyOn(client, "clear");
    root = createRoot(document.createElement("div"));
    await act(async () => root.render(<QueryClientProvider client={client}><MemoryRouter><LoginPage /></MemoryRouter></QueryClientProvider>));
});

afterEach(async () => {
    await act(async () => root.unmount());
    client.clear();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
});

function expectClearedBeforeToken() {
    expect(client.getQueryCache().getAll()).toHaveLength(0);
    expect(state.saveToken).toHaveBeenCalledWith(token);
    expect(clear.mock.invocationCallOrder[0]).toBeLessThan(state.saveToken.mock.invocationCallOrder[0]);
}

describe("admin sign-in session isolation", () => {
    it("clears cached identity and private data before a direct successful login", async () => {
        state.login.mockResolvedValue({ code_required: false, token });
        await act(async () => state.onToken("fixture-captcha"));
        expectClearedBeforeToken();
    });

    it("keeps cache during an email-code challenge and clears it on confirmation", async () => {
        state.login.mockResolvedValue({ code_required: true, session: "fixture-session" });
        state.confirm.mockResolvedValue(token);
        await act(async () => state.onToken("fixture-captcha"));
        expect(clear).not.toHaveBeenCalled();
        await act(async () => state.onCode("123456"));
        expectClearedBeforeToken();
    });

    it("keeps cache during a second-factor challenge and clears it on verification", async () => {
        state.login.mockResolvedValue({ code_required: false, two_fa_required: true, pending_token: "fixture-pending" });
        state.verify.mockResolvedValue(token);
        await act(async () => state.onToken("fixture-captcha"));
        expect(clear).not.toHaveBeenCalled();
        await act(async () => state.onCode("123456"));
        expectClearedBeforeToken();
    });

    it("does not replace the session or clear caches on a failed login", async () => {
        state.login.mockRejectedValue(new Error("fixture-login-failure"));
        await act(async () => state.onToken("fixture-captcha"));
        expect(clear).not.toHaveBeenCalled();
        expect(state.saveToken).not.toHaveBeenCalled();
        expect(client.getQueryCache().getAll()).toHaveLength(2);
    });
});
