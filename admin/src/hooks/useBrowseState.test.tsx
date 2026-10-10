import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import useBrowseState from "./useBrowseState";
import { ADMIN_BROWSE_PREFIX, clearAdminBrowse, resumeBrowse } from "@/lib/browseState";
import { emptyLogFilters, parseLogFilters } from "@/lib/nodeDiagnostics";

const identity = vi.hoisted(() => ({ id: "admin-a" }));
vi.mock("@/hooks/useMe", () => ({ useMe: () => ({ data: { id: identity.id } }) }));
let root: Root;
let host: HTMLDivElement;
let node = "worker-a";
function View() {
    const [value, setValue] = useBrowseState(`node-logs:${node}`, emptyLogFilters, parseLogFilters);
    return <><span>{value.level || "all"}</span><button onClick={() => setValue({ ...value, level: "warn" })}>Warn</button></>;
}
beforeEach(async () => {
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true); identity.id = "admin-a"; node = "worker-a";
    sessionStorage.clear(); resumeBrowse(); host = document.createElement("div"); root = createRoot(host);
});
afterEach(async () => { await act(async () => root.unmount()); sessionStorage.clear(); vi.unstubAllGlobals(); });
const render = () => act(async () => root.render(<View />));
const key = (owner: string, worker: string) => `${ADMIN_BROWSE_PREFIX}${owner}:node-logs:${worker}`;

describe("admin-scoped diagnostic browse state", () => {
    it("restores validated filters on remount and keeps other users/nodes isolated", async () => {
        sessionStorage.setItem(key("admin-a", "worker-a"), JSON.stringify({ value: { ...emptyLogFilters, level: "error" } }));
        await render(); expect(host.textContent).toContain("error");
        identity.id = "admin-b"; await render(); expect(host.textContent).toContain("all");
        await act(async () => host.querySelector("button")!.click());
        expect(JSON.parse(sessionStorage.getItem(key("admin-b", "worker-a"))!).value.level).toBe("warn");
        node = "consumer-a"; await render(); expect(host.textContent).toContain("all");
        identity.id = "admin-a"; node = "worker-a"; await render(); expect(host.textContent).toContain("error");
    });
    it("rejects malformed stored filters without overwriting another user's state", async () => {
        sessionStorage.setItem(key("admin-a", "worker-a"), "{broken");
        sessionStorage.setItem(key("admin-b", "worker-a"), "untouched");
        await render(); expect(host.textContent).toContain("all");
        expect(sessionStorage.getItem(key("admin-b", "worker-a"))).toBe("untouched");
    });
    it("clears on logout and prevents mounted old-session state from reappearing", async () => {
        await render(); await act(async () => host.querySelector("button")!.click());
        expect(sessionStorage.length).toBe(1);
        clearAdminBrowse(); resumeBrowse();
        await act(async () => host.querySelector("button")!.click());
        expect(sessionStorage.length).toBe(0);
    });
});
