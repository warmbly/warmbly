import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { APIError } from "@/lib/api/client";
import type * as ClientModule from "@/lib/api/client";
import DeviceApprovalPage from "./DeviceApprovalPage";

const api = vi.hoisted(() => ({ describe: vi.fn(), decide: vi.fn(), reauth: vi.fn() }));
vi.mock("@/lib/api/client/admin/device", () => ({ describeAdminDevice: api.describe, decideAdminDevice: api.decide }));
vi.mock("@/lib/api/client", async (importOriginal) => ({ ...await importOriginal<typeof ClientModule>(), promptForReauth: api.reauth }));
vi.mock("@/lib/env", () => ({ API_URL: "http://localhost:8080" }));
vi.mock("@/hooks/useMe", () => ({ useMe: () => ({ data: { id: "fixture-admin", admin_permissions: 16 } }) }));

let root: Root;
let host: HTMLDivElement;
const consentToken = "c".repeat(43);
const description = () => ({ request: { client_name: "Fixture laptop", instance_url: "http://localhost:8080", status: "pending", expires_at: new Date(Date.now() + 60000).toISOString(), consent_token: consentToken }, requested_access: "Your live platform administrator permissions through a separate revocable session", admin_permissions: 16 });

beforeEach(async () => {
    vi.clearAllMocks();
    localStorage.clear(); sessionStorage.clear();
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
    api.describe.mockResolvedValue(description());
    api.decide.mockImplementation((_code, _token, decision) => Promise.resolve({ status: decision }));
    api.reauth.mockResolvedValue(undefined);
    host = document.createElement("div"); document.body.append(host); root = createRoot(host);
    await act(async () => root.render(<MemoryRouter initialEntries={["/device?user_code=ABCD-EFGH&approve=true"]}><DeviceApprovalPage /></MemoryRouter>));
});
afterEach(async () => {
    await act(async () => root.unmount()); host.remove(); vi.useRealTimers(); vi.unstubAllGlobals();
});
function button(name: string) {
    const found = [...host.querySelectorAll("button")].find((b) => b.textContent === name);
    if (!found) throw new Error(`Button missing: ${name}`);
    return found;
}
async function review() {
    await act(async () => {
        const input = host.querySelector("input")!;
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!.call(input, "ABCD-EFGH");
        input.dispatchEvent(new Event("input", { bubbles: true }));
    });
    await act(async () => host.querySelector("form")!.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true })));
}

describe("explicit admin device consent", () => {
    it("does not describe or approve URL codes on load", () => {
        expect(api.describe).not.toHaveBeenCalled(); expect(api.decide).not.toHaveBeenCalled();
    });
    it("shows instance/client/capability/expiry without exposing or persisting consent", async () => {
        await review();
        expect(api.describe).toHaveBeenCalledWith("ABCD-EFGH");
        expect(api.decide).not.toHaveBeenCalled();
        expect(host.textContent).toContain("Fixture laptop"); expect(host.textContent).toContain("http://localhost:8080");
        expect(host.textContent).toContain("live platform administrator permissions"); expect(host.textContent).toContain("Expires");
        expect(host.innerHTML).not.toContain(consentToken); expect(localStorage.length).toBe(0); expect(sessionStorage.length).toBe(0);
        await act(async () => button("Approve CLI sign-in").click());
        expect(api.decide).toHaveBeenCalledWith("ABCD-EFGH", consentToken, "approved");
        expect(host.textContent).toContain("Request approved");
    });
    it("supports an explicit denial instead of an approval", async () => {
        await review(); await act(async () => button("Deny request").click());
        expect(api.decide).toHaveBeenCalledWith("ABCD-EFGH", consentToken, "denied");
        expect(host.textContent).toContain("Request denied");
    });
    it("requires a new explicit decision after native reauthentication", async () => {
        api.decide.mockRejectedValueOnce(new APIError("Confirm identity", 403, { code: "reauth_required" }));
        await review(); await act(async () => button("Approve CLI sign-in").click());
        expect(api.decide).toHaveBeenCalledTimes(1); expect(api.reauth).not.toHaveBeenCalled();
        await act(async () => button("Confirm identity").click());
        expect(api.reauth).toHaveBeenCalledTimes(1); expect(api.decide).toHaveBeenCalledTimes(1);
        await act(async () => button("Approve CLI sign-in").click());
        expect(api.decide).toHaveBeenCalledTimes(2);
    });
    it("expires pending consent without an automatic decision", async () => {
        vi.useFakeTimers(); await review();
        await act(async () => { vi.advanceTimersByTime(60001); });
        expect(host.textContent).toContain("Request expired"); expect(api.decide).not.toHaveBeenCalled();
    });
    it.each(["admin_device_expired", "admin_device_resolved"])("keeps %s fail-closed", async (code) => {
        api.decide.mockRejectedValueOnce(new APIError("Cannot use code", code.endsWith("expired") ? 404 : 409, { code }));
        await review(); await act(async () => button("Approve CLI sign-in").click());
        expect(host.textContent).toContain(code.endsWith("expired") ? "Request expired" : "Request already resolved");
        expect(host.textContent).not.toContain("Request approved");
    });
    it.each(["https://other.example.test", "http://user:password@localhost:8080", "http://localhost:8080?token=hidden"])("withholds consent for ambiguous/mismatched instance %s", async (instance_url) => {
        api.describe.mockResolvedValue({ ...description(), request: { ...description().request, instance_url } });
        await review(); expect(host.textContent).toContain("could not be verified"); expect(host.innerHTML).not.toContain(instance_url); expect(api.decide).not.toHaveBeenCalled();
    });
    it("does not silently retry an uncertain failed decision", async () => {
        api.decide.mockRejectedValueOnce(new Error("Network unavailable"));
        await review(); await act(async () => button("Approve CLI sign-in").click());
        expect(api.decide).toHaveBeenCalledTimes(1); expect(host.textContent).toContain("Review request"); expect(host.textContent).not.toContain("Request approved");
    });
});
