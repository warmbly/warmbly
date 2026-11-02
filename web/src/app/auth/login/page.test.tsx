import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import LoginPage from "./page";

const mocks = vi.hoisted(() => ({
    navigate: vi.fn(),
    register: vi.fn(),
    confirm: vi.fn(),
    login: vi.fn(),
    evaluate: vi.fn().mockResolvedValue({ score: 4, warning: "", suggestions: [] }),
    config: {} as Record<string, unknown>,
}));
const baseConfig = { registration: "invite_only", captcha: false, passkeys: false, password_login: true, providers: [] as string[], mail_delivers: true, brand: { name: "Warmbly" } };

vi.mock("@tanstack/react-router", () => ({
    useNavigate: () => mocks.navigate,
    useLocation: () => ({ pathname: window.location.pathname, searchStr: window.location.search, state: {} }),
    Link: ({ children }: { children: ReactNode }) => <span>{children}</span>,
}));
vi.mock("@/lib/api/hooks/auth/useAuthConfig", () => ({ default: () => ({
    ready: true, unreachable: false,
    config: mocks.config,
}) }));
vi.mock("@/hooks/usePasswordStrength", () => ({ usePasswordStrength: () => ({ evaluate: mocks.evaluate, loading: false }) }));
vi.mock("@/lib/api/hooks/auth/useRegister", () => ({ default: () => ({ isPending: false, mutateAsync: mocks.register }) }));
vi.mock("@/lib/api/hooks/auth/useRegisterConfirm", () => ({ default: () => ({ isPending: false, mutateAsync: mocks.confirm }) }));
vi.mock("@/lib/api/hooks/auth/useLogin", () => ({ default: () => ({ isPending: false, mutateAsync: mocks.login }) }));
vi.mock("@/lib/auth", () => ({ saveTokens: vi.fn(), takeSessionEndedNotice: vi.fn(() => false), SESSION_ENDED_MESSAGE: "" }));
vi.mock("@/lib/api/client/auth/getUser", () => ({ default: vi.fn().mockResolvedValue({ id: "invitee" }) }));
vi.mock("@/components/ui/input-otp", () => ({
    InputOTP: ({ value, onChange }: { value: string; onChange: (value: string) => void }) => <input aria-label="Verification code" value={value} onChange={(event) => onChange(event.target.value)} />,
    InputOTPGroup: () => null,
    InputOTPSlot: () => null,
}));

const token = { access_token: "access", refresh_token: "refresh" };

beforeEach(() => {
    vi.clearAllMocks();
    mocks.config = { ...baseConfig };
    mocks.register.mockResolvedValue({ code_required: false, token });
    mocks.confirm.mockResolvedValue({ code_required: false, token });
    mocks.login.mockResolvedValue({ code_required: false, token });
});
afterEach(cleanup);

function mount(mode: "login" | "register" = "register") {
    const params = new URLSearchParams({ invite: "pending-token", email: "invitee@acme.com", next: "/invite?token=pending-token" });
    window.history.replaceState({}, "", `/auth/${mode}?${params}`);
    render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><LoginPage /></QueryClientProvider>);
}

async function submitSignup() {
    fireEvent.change(screen.getByPlaceholderText("name@company.com"), { target: { value: "invitee@acme.com" } });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    fireEvent.change(await screen.findByPlaceholderText("Create a password"), { target: { value: "A strong signup password 908!" } });
    fireEvent.change(screen.getByPlaceholderText("Confirm your password"), { target: { value: "A strong signup password 908!" } });
    fireEvent.click(screen.getByRole("checkbox"));
    fireEvent.click(screen.getByRole("button", { name: "Create account" }));
    await waitFor(() => expect(mocks.register).toHaveBeenCalledWith(expect.objectContaining({ invite: "pending-token" })));
}

async function submitLogin() {
    fireEvent.change(await screen.findByPlaceholderText("Enter your password"), { target: { value: "Existing password 908!" } });
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
}

describe("invited signup completion", () => {
    it("enters the dashboard directly when email verification is disabled", async () => {
        mount();
        await submitSignup();
        await waitFor(() => expect(mocks.navigate).toHaveBeenCalledWith({ to: "/app/emails", search: {} }));
    });

    it("enters the dashboard after email-code confirmation", async () => {
        mocks.register.mockResolvedValue({ code_required: true, session: "verification-session" });
        mount();
        await submitSignup();
        fireEvent.change(await screen.findByLabelText("Verification code"), { target: { value: "123456" } });
        fireEvent.click(screen.getByRole("button", { name: "Verify" }));
        await waitFor(() => expect(mocks.confirm).toHaveBeenCalledWith({ session: "verification-session", code: "123456" }));
        await waitFor(() => expect(mocks.navigate).toHaveBeenCalledWith({ to: "/app/emails", search: {} }));
    });

    it("does not send a later sign-in back to an invitation consumed by tokenless signup", async () => {
        mocks.register.mockResolvedValue({ code_required: false });
        mount();
        await submitSignup();
        await submitLogin();
        await waitFor(() => expect(mocks.navigate).toHaveBeenCalledWith({ to: "/app/emails", search: {} }));
        expect(new URLSearchParams(window.location.search).has("invite")).toBe(false);
    });

    it("preserves the pending invitation return path for an existing account", async () => {
        mount("login");
        fireEvent.change(screen.getByPlaceholderText("name@company.com"), { target: { value: "invitee@acme.com" } });
        fireEvent.click(screen.getByRole("button", { name: "Continue" }));
        await submitLogin();
        await waitFor(() => expect(mocks.navigate).toHaveBeenCalledWith({ to: "/invite", search: { token: "pending-token" } }));
        expect(mocks.register).not.toHaveBeenCalled();
    });

    it("removes the consumed invitation when confirmed signup cannot issue a token", async () => {
        mocks.register.mockResolvedValue({ code_required: true, session: "verification-session" });
        mocks.confirm.mockResolvedValue({ code_required: false });
        mount();
        await submitSignup();
        fireEvent.change(await screen.findByLabelText("Verification code"), { target: { value: "123456" } });
        fireEvent.click(screen.getByRole("button", { name: "Verify" }));
        fireEvent.click(await screen.findByRole("button", { name: "Continue" }));
        await submitLogin();
        await waitFor(() => expect(mocks.navigate).toHaveBeenCalledWith({ to: "/app/emails", search: {} }));
        expect(new URLSearchParams(window.location.search).has("invite")).toBe(false);
    });
});

describe("sign-in with password login disabled", () => {
    function enterEmail() {
        window.history.replaceState({}, "", "/auth/login");
        render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><LoginPage /></QueryClientProvider>);
        fireEvent.change(screen.getByPlaceholderText("name@company.com"), { target: { value: "member@acme.com" } });
        fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    }

    it("offers only single sign-on, with no password field or reset link", async () => {
        mocks.config = { ...baseConfig, password_login: false, providers: ["oidc"], provider_labels: { oidc: "Acme SSO" } };
        enterEmail();
        expect(await screen.findByRole("button", { name: "Continue with Acme SSO" })).toBeTruthy();
        expect(screen.queryByPlaceholderText("Enter your password")).toBeNull();
        expect(screen.queryByText("Forgot password?")).toBeNull();
        expect(screen.queryByRole("button", { name: "Sign in" })).toBeNull();
    });

    it("explains that password sign-in is off when no provider is configured", async () => {
        mocks.config = { ...baseConfig, password_login: false, providers: [] };
        enterEmail();
        expect(await screen.findByText("Password sign-in is turned off on this server")).toBeTruthy();
        expect(screen.queryByPlaceholderText("Enter your password")).toBeNull();
    });

    it("keeps the password form when password login is enabled", async () => {
        mocks.config = { ...baseConfig, providers: ["oidc"] };
        enterEmail();
        expect(await screen.findByPlaceholderText("Enter your password")).toBeTruthy();
        expect(screen.getByRole("button", { name: "Continue with single sign-on" })).toBeTruthy();
    });
});
