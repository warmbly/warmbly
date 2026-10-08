import { act, renderHook } from "@testing-library/react";
import { type ComponentProps, type ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { z } from "zod";
import { UserContext } from "./context/user";
import { useAppStore } from "@/stores/useAppStore";
import { BROWSE_STORAGE_PREFIX, clearBrowseState } from "@/lib/browseState";
import useBrowseState from "./useBrowseState";

let userID = "user";
const key = (name = "tasks", owner = "user", organization = "workspace") => `${BROWSE_STORAGE_PREFIX}${owner}:${organization}:${name}`;
const schema = z.object({ query: z.string(), status: z.enum(["all", "pending", "done"]), reverse: z.boolean() });
const defaults = { query: "", status: "all" as const, reverse: false };

function Owner({ children }: { children: ReactNode }) {
    const profile = userID ? { user: { id: userID } } as ComponentProps<typeof UserContext.Provider>["value"] : null;
    return <UserContext.Provider value={profile}>{children}</UserContext.Provider>;
}

describe("workspace-scoped browse state", () => {
    beforeEach(() => {
        vi.restoreAllMocks();
        sessionStorage.clear();
        userID = "user";
        useAppStore.setState({ currentOrganization: { id: "workspace", name: "Workspace", role: "owner" } });
    });

    it("restores filters and sort on a fresh mount without first rendering defaults", () => {
        const page = renderHook(() => useBrowseState("tasks", defaults, schema), { wrapper: Owner });
        act(() => page.result.current[1]({ query: "alice", status: "pending", reverse: true }));
        page.unmount();
        const refreshed = renderHook(() => useBrowseState("tasks", defaults, schema), { wrapper: Owner });
        expect(refreshed.result.current[0]).toEqual({ query: "alice", status: "pending", reverse: true });
    });

    it("supports consecutive functional updates and remembers explicit clearing", () => {
        const page = renderHook(() => useBrowseState("tasks", defaults, schema), { wrapper: Owner });
        act(() => {
            page.result.current[1]((previous) => ({ ...previous, query: "alice" }));
            page.result.current[1]((previous) => ({ ...previous, reverse: true }));
        });
        expect(page.result.current[0]).toEqual({ query: "alice", status: "all", reverse: true });
        act(() => page.result.current[1](defaults));
        page.unmount();
        const refreshed = renderHook(() => useBrowseState("tasks", defaults, schema), { wrapper: Owner });
        expect(refreshed.result.current[0]).toEqual(defaults);
    });

    it("gives an explicit navigation intent priority over remembered filters", () => {
        sessionStorage.setItem(key(), JSON.stringify({ value: { ...defaults, query: "remembered" } }));
        const intent = { ...defaults, query: "linked" };
        const page = renderHook(() => useBrowseState("tasks", defaults, schema, { initialOverride: intent }), { wrapper: Owner });
        expect(page.result.current[0].query).toBe("linked");
        act(() => page.result.current[1](defaults));
        expect(page.result.current[0]).toEqual(defaults);
    });

    it("isolates pages, workspaces and users while the view stays mounted", () => {
        sessionStorage.setItem(key("tasks", "user", "another"), JSON.stringify({ value: { ...defaults, query: "receiving" } }));
        const page = renderHook(({ name }) => useBrowseState(name, defaults, schema), { wrapper: Owner, initialProps: { name: "tasks" } });
        act(() => page.result.current[1]({ ...defaults, query: "sending" }));
        const staleSetter = page.result.current[1];
        act(() => useAppStore.setState({ currentOrganization: { id: "another", name: "Another", role: "owner" } }));
        expect(page.result.current[0].query).toBe("receiving");
        act(() => staleSetter(defaults));
        expect(page.result.current[0].query).toBe("receiving");
        expect(JSON.parse(sessionStorage.getItem(key())!).value.query).toBe("sending");
        userID = "another-user";
        page.rerender({ name: "tasks" });
        expect(page.result.current[0]).toEqual(defaults);
        userID = "user";
        page.rerender({ name: "tasks" });
        expect(page.result.current[0].query).toBe("receiving");
        page.rerender({ name: "campaigns" });
        expect(page.result.current[0]).toEqual(defaults);
        page.rerender({ name: "tasks" });
        expect(page.result.current[0].query).toBe("receiving");
    });

    it.each(["{broken", JSON.stringify({ value: { query: 123, status: "pending", reverse: false } }), JSON.stringify({ value: { ...defaults, status: "unknown" } })])("ignores malformed or stale stored filters: %s", (saved) => {
        sessionStorage.setItem(key(), saved);
        const page = renderHook(() => useBrowseState("tasks", () => defaults, schema), { wrapper: Owner });
        expect(page.result.current[0]).toEqual(defaults);
    });

    it("revives typed dates and keeps an explicitly cleared optional field", () => {
        const dateSchema = z.object({ since: z.coerce.date().optional() });
        const initial = { since: new Date("2026-10-01T00:00:00Z") };
        const page = renderHook(() => useBrowseState("history", initial, dateSchema), { wrapper: Owner });
        page.unmount();
        const refreshed = renderHook(() => useBrowseState("history", initial, dateSchema), { wrapper: Owner });
        expect(refreshed.result.current[0].since).toEqual(initial.since);
        act(() => refreshed.result.current[1]({ since: undefined }));
        refreshed.unmount();
        const cleared = renderHook(() => useBrowseState("history", initial, dateSchema), { wrapper: Owner });
        expect(cleared.result.current[0].since).toBeUndefined();
    });

    it("continues working with blocked or full storage", () => {
        vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => { throw new Error("blocked"); });
        vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => { throw new Error("full"); });
        const page = renderHook(() => useBrowseState("tasks", defaults, schema), { wrapper: Owner });
        act(() => page.result.current[1]({ ...defaults, query: "still works" }));
        expect(page.result.current[0].query).toBe("still works");
    });

    it("does not save anonymous browsing state", () => {
        userID = "";
        const page = renderHook(() => useBrowseState("tasks", defaults, schema), { wrapper: Owner });
        act(() => page.result.current[1]({ ...defaults, query: "anonymous" }));
        expect(sessionStorage.getItem(key())).toBeNull();
    });

    it("removes browsing preferences and legacy tags on logout without clearing other storage", () => {
        sessionStorage.setItem(key(), "private search");
        sessionStorage.setItem("warmbly:mailbox-tag:user:workspace", "tag");
        sessionStorage.setItem("other-feature", "preserve");
        clearBrowseState();
        expect(sessionStorage.getItem(key())).toBeNull();
        expect(sessionStorage.getItem("warmbly:mailbox-tag:user:workspace")).toBeNull();
        expect(sessionStorage.getItem("other-feature")).toBe("preserve");
    });
});
