import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ConfirmProvider from "@/hooks/ConfirmProvider";
import { useConfirm } from "@/hooks/context/confirm";
import Client from "@/lib/api/client/Client";
import { deleteGrant } from "@/lib/api/client/app/emails/imports/mailboxGrants";
import getToken from "@/lib/helper/getToken";
import type ReauthMethods from "@/lib/api/models/auth/ReauthMethods";
import ReauthModal from "./ReauthModal";

const navigate = vi.fn();
const logout = vi.fn(() => Promise.resolve());
const methods = vi.fn<() => Promise<ReauthMethods>>();
const reauth = vi.fn();
const beginSSO = vi.fn();
const finishSSO = vi.fn();

vi.mock("@tanstack/react-router", () => ({ useNavigate: () => navigate }));
vi.mock("@/lib/api/client/Client", () => ({ default: { request: vi.fn() } }));
vi.mock("@/lib/helper/getToken", () => ({ default: vi.fn() }));
vi.mock("@/lib/api/hooks/auth/useLogout", () => ({ default: () => ({ mutateAsync: logout, isPending: false }) }));
vi.mock("@/lib/api/client/auth/reauth", () => ({ default: (...a: unknown[]) => reauth(...a) }));
vi.mock("@/lib/api/client/auth/reauth/methods", () => ({ default: () => methods() }));
vi.mock("@/lib/api/client/auth/reauth/passkey", () => ({ beginPasskeyReauth: () => Promise.resolve({ options: { publicKey: {} } }) }));
vi.mock("@/lib/api/client/auth/reauth/sso", () => ({
    beginSSOReauth: (...a: unknown[]) => beginSSO(...a),
    finishSSOReauth: (...a: unknown[]) => finishSSO(...a),
}));
vi.mock("@/lib/passkey", () => ({
    PasskeyCancelled: class extends Error {},
    confirmWithPasskey: vi.fn(),
    passkeySupported: () => true,
}));

const none: ReauthMethods = { password: false, totp: false, passkey: false, providers: [], native_providers: [], valid_for_seconds: 300 };

function ask() {
    const resolve = vi.fn();
    const reject = vi.fn();
    act(() => {
        window.dispatchEvent(new CustomEvent("reauth-required", { detail: { resolve, reject } }));
    });
    return { resolve, reject };
}

function RemoveGrant() {
    const confirm = useConfirm();
    return <button onClick={() => confirm.show("Remove this admin grant?", () => deleteGrant("test-grant"))}>Remove grant</button>;
}

function removalDialog() {
    return screen.getByText("Remove this admin grant?").closest('[role="alertdialog"]') as HTMLElement;
}

function overlayLayer(dialog: HTMLElement) {
    const layer = dialog.parentElement?.className.match(/z-\[(\d+)\]/)?.[1];
    expect(layer).toBeDefined();
    return Number(layer);
}

describe("ReauthModal", () => {
    it.each([
        ["two_fa_code_used", "Wait for a new authenticator code, then try again."],
        ["internal_error", "We couldn't verify your confirmation right now. Try again shortly."],
        ["service_unavailable", "We couldn't verify your confirmation right now. Try again shortly."],
        [undefined, "We couldn't verify your confirmation right now. Try again shortly."],
        ["credentials", "That did not match. Try again."],
    ])("explains %s without confirming the waiting action", async (code, message) => {
        methods.mockResolvedValue({ ...none, totp: true });
        reauth.mockRejectedValue({ code });
        render(<ReauthModal />);
        const { resolve, reject } = ask();
        fireEvent.change(await screen.findByLabelText("Two-factor or recovery code"), { target: { value: "123456" } });
        fireEvent.click(screen.getByRole("button", { name: "Confirm" }));
        expect(await screen.findByRole("alert")).toHaveTextContent(message);
        expect(resolve).not.toHaveBeenCalled();
        expect(reject).not.toHaveBeenCalled();
    });

    beforeEach(() => {
        vi.clearAllMocks();
        vi.mocked(getToken).mockReturnValue({
            access_token: "access-token",
            access_token_expires_at: new Date(Date.now() + 60_000),
            refresh_token: "refresh-token",
            refresh_token_expires_at: new Date(Date.now() + 120_000),
        });
    });
    afterEach(() => {
        vi.restoreAllMocks();
    });

    it("offers a Google-only account its provider and no password field", async () => {
        methods.mockResolvedValue({ ...none, providers: [{ id: "google", label: "Google" }] });
        render(<ReauthModal />);
        ask();
        expect(await screen.findByRole("button", { name: "Continue with Google" })).toBeInTheDocument();
        expect(screen.queryByLabelText("Password")).not.toBeInTheDocument();
        expect(screen.queryByRole("button", { name: "Confirm" })).not.toBeInTheDocument();
    });

    it("confirms through the provider window and retries the waiting request", async () => {
        methods.mockResolvedValue({ ...none, providers: [{ id: "google", label: "Google" }] });
        const popup = { closed: false, location: { href: "about:blank", replace: vi.fn() }, document: { body: { childElementCount: 1 } }, focus: vi.fn(), close: vi.fn() };
        vi.spyOn(window, "open").mockReturnValue(popup as unknown as Window);
        beginSSO.mockResolvedValue({ url: "https://accounts.google.com/x", binding: "kept-here" });
        finishSSO.mockResolvedValue({ valid_for_seconds: 300 });

        render(<ReauthModal />);
        const { resolve, reject } = ask();
        fireEvent.click(await screen.findByRole("button", { name: "Continue with Google" }));
        await waitFor(() => expect(popup.location.replace).toHaveBeenCalledWith("https://accounts.google.com/x"));

        act(() => {
            window.dispatchEvent(new MessageEvent("message", {
                origin: window.location.origin,
                data: { type: "warmbly_reauth", code: "handoff", error: "", cancelled: false },
            }));
        });
        await waitFor(() => expect(resolve).toHaveBeenCalled());
        expect(finishSSO).toHaveBeenCalledWith("handoff", "kept-here");
        expect(reject).not.toHaveBeenCalled();
    });

    it("ignores a result posted from another origin", async () => {
        methods.mockResolvedValue({ ...none, providers: [{ id: "google", label: "Google" }] });
        const popup = { closed: false, location: { href: "about:blank", replace: vi.fn() }, document: { body: { childElementCount: 1 } }, focus: vi.fn(), close: vi.fn() };
        vi.spyOn(window, "open").mockReturnValue(popup as unknown as Window);
        beginSSO.mockResolvedValue({ url: "https://accounts.google.com/x", binding: "b" });

        render(<ReauthModal />);
        const { resolve } = ask();
        fireEvent.click(await screen.findByRole("button", { name: "Continue with Google" }));
        await waitFor(() => expect(popup.location.replace).toHaveBeenCalled());
        act(() => {
            window.dispatchEvent(new MessageEvent("message", {
                origin: "https://evil.example",
                data: { type: "warmbly_reauth", code: "handoff", error: "", cancelled: false },
            }));
        });
        await new Promise((r) => setTimeout(r, 20));
        expect(finishSSO).not.toHaveBeenCalled();
        expect(resolve).not.toHaveBeenCalled();
    });

    it("explains the way out to an account with nothing to confirm with", async () => {
        methods.mockResolvedValue(none);
        render(<ReauthModal />);
        const { reject } = ask();
        expect(await screen.findByText(/no password, passkey or authenticator yet/)).toBeInTheDocument();
        expect(screen.queryByLabelText("Password")).not.toBeInTheDocument();

        fireEvent.click(screen.getByRole("button", { name: "Sign in again" }));
        await waitFor(() => expect(navigate).toHaveBeenCalledWith(expect.objectContaining({ to: "/auth/login" })));
        expect(logout).toHaveBeenCalled();
        expect(reject).toHaveBeenCalled();
    });

    it("falls back to both fields when the methods lookup fails", async () => {
        methods.mockRejectedValue(new Error("offline"));
        render(<ReauthModal />);
        ask();
        expect(await screen.findByLabelText("Password")).toBeInTheDocument();
        expect(screen.getByLabelText("Two-factor or recovery code")).toBeInTheDocument();
    });

    it("answers every request that was waiting with one confirmation", async () => {
        methods.mockResolvedValue({ ...none, password: true });
        reauth.mockResolvedValue({ valid_for_seconds: 300 });
        render(<ReauthModal />);
        const first = ask();
        const second = ask();

        fireEvent.change(await screen.findByLabelText("Password"), { target: { value: "hunter22" } });
        fireEvent.click(screen.getByRole("button", { name: "Confirm" }));
        await waitFor(() => expect(first.resolve).toHaveBeenCalled());
        expect(second.resolve).toHaveBeenCalled();
        expect(screen.queryByLabelText("Two-factor or recovery code")).not.toBeInTheDocument();
    });

    it("rejects every waiting request on Escape", async () => {
        methods.mockResolvedValue({ ...none, password: true });
        render(<ReauthModal />);
        const first = ask();
        const second = ask();
        await screen.findByLabelText("Password");
        fireEvent.keyDown(window, { key: "Escape" });
        expect(first.reject).toHaveBeenCalled();
        expect(second.reject).toHaveBeenCalled();
    });

    it("places identity confirmation above an awaiting grant removal and retries the delete once", async () => {
        methods.mockResolvedValue({ ...none, password: true });
        reauth.mockResolvedValue({ valid_for_seconds: 300 });
        vi.mocked(Client.request)
            .mockRejectedValueOnce({ status: 403, code: "reauth_required" })
            .mockResolvedValueOnce({ data: undefined });
        render(<ConfirmProvider><RemoveGrant /><ReauthModal /></ConfirmProvider>);

        fireEvent.click(screen.getByRole("button", { name: "Remove grant" }));
        fireEvent.click(within(removalDialog()).getByRole("button", { name: "Confirm" }));
        const password = await screen.findByLabelText("Password");
        const identity = screen.getByRole("alertdialog", { name: "Confirm it is you" });
        expect(overlayLayer(identity)).toBeGreaterThan(overlayLayer(removalDialog()));
        expect(within(removalDialog()).getByRole("button", { name: "Confirm" })).toBeDisabled();
        expect(Client.request).toHaveBeenCalledTimes(1);

        fireEvent.change(password, { target: { value: "test-password" } });
        fireEvent.click(within(identity).getByRole("button", { name: "Confirm" }));

        await waitFor(() => expect(Client.request).toHaveBeenCalledTimes(2));
        expect(reauth).toHaveBeenCalledWith({ password: "test-password", code: undefined });
        expect(vi.mocked(Client.request).mock.calls.map(([config]) => [config.method, config.url]))
            .toEqual([["DELETE", "/emails/grants/test-grant"], ["DELETE", "/emails/grants/test-grant"]]);
        await waitFor(() => expect(screen.queryAllByRole("alertdialog")).toHaveLength(0));
    });

    it("leaves grant removal retryable without resending when identity confirmation is cancelled", async () => {
        methods.mockResolvedValue({ ...none, password: true });
        vi.mocked(Client.request).mockRejectedValueOnce({ status: 403, code: "reauth_required" });
        render(<ConfirmProvider><RemoveGrant /><ReauthModal /></ConfirmProvider>);

        fireEvent.click(screen.getByRole("button", { name: "Remove grant" }));
        fireEvent.click(within(removalDialog()).getByRole("button", { name: "Confirm" }));
        await screen.findByLabelText("Password");
        fireEvent.keyDown(window, { key: "Escape" });

        await waitFor(() => expect(within(removalDialog()).getByRole("button", { name: "Confirm" })).toBeEnabled());
        expect(Client.request).toHaveBeenCalledTimes(1);
        expect(reauth).not.toHaveBeenCalled();
        await waitFor(() => expect(screen.queryByRole("alertdialog", { name: "Confirm it is you" })).not.toBeInTheDocument());
    });

    it("does not treat a permission refusal as an identity check or retry the deletion", async () => {
        vi.mocked(Client.request).mockRejectedValueOnce({ status: 403, code: "forbidden", message: "Not allowed" });
        render(<ConfirmProvider><RemoveGrant /><ReauthModal /></ConfirmProvider>);

        fireEvent.click(screen.getByRole("button", { name: "Remove grant" }));
        fireEvent.click(within(removalDialog()).getByRole("button", { name: "Confirm" }));

        await waitFor(() => expect(within(removalDialog()).getByRole("button", { name: "Confirm" })).toBeEnabled());
        expect(Client.request).toHaveBeenCalledTimes(1);
        expect(methods).not.toHaveBeenCalled();
        expect(screen.queryByRole("alertdialog", { name: "Confirm it is you" })).not.toBeInTheDocument();
    });
});
