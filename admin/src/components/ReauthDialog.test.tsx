import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { APIError } from "@/lib/api/client";
import { ReauthDialog } from "./ReauthDialog";

const api = vi.hoisted(() => ({ reauth: vi.fn(), methods: vi.fn() }));
vi.mock("@/lib/api/client/auth", () => ({ reauth: api.reauth, reauthMethods: api.methods }));
vi.mock("@/lib/env", () => ({ API_URL: "http://localhost:8080" }));

let root: Root;
let host: HTMLDivElement;
beforeEach(() => {
    vi.clearAllMocks();
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
    api.methods.mockResolvedValue({ password: false, totp: true });
    host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);
});
afterEach(async () => {
    await act(async () => root.unmount());
    host.remove();
    vi.unstubAllGlobals();
});

describe("ReauthDialog", () => {
    it.each([
        ["two_fa_code_used", "Wait for a new authenticator code, then try again."],
        ["internal_error", "We couldn't verify your confirmation right now. Try again shortly."],
        ["service_unavailable", "We couldn't verify your confirmation right now. Try again shortly."],
        [undefined, "We couldn't verify your confirmation right now. Try again shortly."],
        ["credentials", "That did not match. Try again."],
    ])("explains %s without confirming the waiting action", async (code, message) => {
        api.reauth.mockRejectedValue(code ? new APIError("Failure", 400, { code }) : new Error("offline"));
        const resolve = vi.fn();
        const reject = vi.fn();
        await act(async () => root.render(<ReauthDialog />));
        await act(async () => window.dispatchEvent(new CustomEvent("reauth-required", { detail: { resolve, reject } })));
        const input = document.querySelector('input[autocomplete="one-time-code"]');
        if (!(input instanceof HTMLInputElement)) throw new Error("Missing code field");
        const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
        if (!setter) throw new Error("Missing input setter");
        await act(async () => {
            setter.call(input, "123456");
            input.dispatchEvent(new Event("input", { bubbles: true }));
        });
        const form = input.closest("form");
        if (!form) throw new Error("Missing confirmation form");
        await act(async () => form.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true })));
        expect(document.querySelector('[role="alert"]')?.textContent).toBe(message);
        expect(api.reauth).toHaveBeenCalledWith({ password: undefined, code: "123456" });
        expect(resolve).not.toHaveBeenCalled();
        expect(reject).not.toHaveBeenCalled();
    });
});
