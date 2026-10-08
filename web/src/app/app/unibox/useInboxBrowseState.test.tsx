import { act, cleanup, renderHook } from "@testing-library/react";
import { type ComponentProps, type ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { UserContext } from "@/hooks/context/user";
import { useAppStore } from "@/stores/useAppStore";
import { BROWSE_STORAGE_PREFIX } from "@/lib/browseState";
import { inboxListSchema, inboxPresetDate } from "@/lib/browse-inbox";
import type { UniboxSearchParams } from "@/lib/api/models/app/unibox/UniboxSearch";
import useInboxBrowseState from "./useInboxBrowseState";
import useInboxDebouncedValue from "@/hooks/useBrowseDebouncedValue";

function Owner({ children }: { children: ReactNode }) {
  return <UserContext.Provider value={{ user: { id: "user" } } as ComponentProps<typeof UserContext.Provider>["value"]}>{children}</UserContext.Provider>;
}
const key = `${BROWSE_STORAGE_PREFIX}user:workspace:unibox.list.browsing`;
const all: UniboxSearchParams = { includeArchived: true };
const mount = (scope = "all", base = all, accounts: { id: string; tags?: string[] }[] = []) => renderHook(
  (props) => useInboxBrowseState(props.scope, props.base, props.accounts),
  { wrapper: Owner, initialProps: { scope, base, accounts } },
);

beforeEach(() => {
  sessionStorage.clear();
  useAppStore.setState({ currentOrganization: { id: "workspace", name: "Workspace", role: "owner" } });
});
afterEach(() => { cleanup(); vi.useRealTimers(); });

describe("inbox canonical browsing", () => {
  it("restores all user filters, search, sort and real custom dates on the first refreshed render", () => {
    const page = mount();
    const since = new Date("2026-09-01T00:00:00Z");
    const until = new Date("2026-09-30T00:00:00Z");
    act(() => {
      page.result.current.setParams((previous) => ({ ...previous, from: "sender", unseen: false, since, until, datePreset: "custom", categoryIds: ["label"], accountIds: ["mailbox"], sortBy: "oldest", cursor: "never-save", query: "mirror", limit: 50 }));
      page.result.current.setSearch("subject");
    });
    page.unmount();
    const refreshed = mount();
    expect(refreshed.result.current.params).toMatchObject({ from: "sender", unseen: false, since, until, categoryIds: ["label"], accountIds: ["mailbox"], sortBy: "oldest", includeArchived: true });
    expect(refreshed.result.current.params.since).toBeInstanceOf(Date);
    expect(refreshed.result.current.search).toBe("subject");
    const stored = JSON.parse(sessionStorage.getItem(key)!);
    expect(stored.value.filters).not.toHaveProperty("cursor");
    expect(stored.value.filters).not.toHaveProperty("query");
    expect(stored.value.filters).not.toHaveProperty("limit");
    expect(stored.value.filters).not.toHaveProperty("includeArchived");
  });

  it("recomposes a selected tag from current membership instead of saving mailbox ids", () => {
    const page = mount("all", all, [{ id: "old", tags: ["tag"] }]);
    act(() => page.result.current.setParams((previous) => ({ ...previous, tagId: "tag", accountIds: ["old"] })));
    expect(JSON.parse(sessionStorage.getItem(key)!).value.filters).toEqual({ tagId: "tag" });
    page.unmount();
    const refreshed = mount("all", all, [{ id: "new", tags: ["tag"] }]);
    expect(refreshed.result.current.params.accountIds).toEqual(["new"]);
    refreshed.rerender({ scope: "all", base: all, accounts: [] });
    expect(refreshed.result.current.params.tagId).toBe("tag");
    expect(refreshed.result.current.params.accountIds).toEqual([]);
  });

  it("metadata arriving for tag and premade-view scopes does not reset restored extra filters", () => {
    const page = mount("tag:tag", { accountIds: [], tagId: "tag" });
    act(() => { page.result.current.setParams((previous) => ({ ...previous, from: "sender", sortBy: "oldest" })); page.result.current.setSearch("subject"); });
    page.unmount();
    const refreshed = mount("tag:tag", { accountIds: [], tagId: "tag" });
    refreshed.rerender({ scope: "tag:tag", base: { accountIds: ["loaded"], tagId: "tag" }, accounts: [] });
    expect(refreshed.result.current.params).toMatchObject({ from: "sender", sortBy: "oldest", accountIds: ["loaded"] });
    expect(refreshed.result.current.search).toBe("subject");
    refreshed.rerender({ scope: "view:interested", base: { categoryIds: [] }, accounts: [] });
    act(() => refreshed.result.current.setParams((previous) => ({ ...previous, from: "view-sender" })));
    refreshed.rerender({ scope: "view:interested", base: { categoryIds: ["current-category"] }, accounts: [] });
    expect(refreshed.result.current.params).toMatchObject({ from: "view-sender", categoryIds: ["current-category"] });
  });

  it("preserves deliberate scope resets, URL priority and Search all mail", () => {
    const page = mount();
    act(() => { page.result.current.setParams((previous) => ({ ...previous, from: "sender", unseen: false, sortBy: "oldest" })); page.result.current.setSearch("subject"); });
    page.unmount();
    const linked = mount("unread", { unseen: true, automated: false });
    expect(linked.result.current.params).toMatchObject({ unseen: true, sortBy: "oldest", automated: false });
    expect(linked.result.current.params.from).toBeUndefined();
    expect(linked.result.current.search).toBe("");
    act(() => linked.result.current.setSearch("across scopes"));
    linked.result.current.keepSearch.current = true;
    linked.rerender({ scope: "all", base: all, accounts: [] });
    expect(linked.result.current.search).toBe("across scopes");
    linked.rerender({ scope: "folder:sent", base: { folder: "sent" }, accounts: [] });
    expect(linked.result.current.search).toBe("");
    expect(linked.result.current.params.sortBy).toBe("oldest");
  });

  it("does not freeze Today scope or relative user-selected dates across refresh", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-10-08T12:00:00Z"));
    const page = mount("today", { since: inboxPresetDate("today") });
    act(() => page.result.current.setParams((previous) => ({ ...previous, from: "sender" })));
    expect(JSON.parse(sessionStorage.getItem(key)!).value.filters).toEqual({ from: "sender" });
    page.unmount();
    vi.setSystemTime(new Date("2026-10-09T12:00:00Z"));
    const refreshed = mount("today", { since: inboxPresetDate("today") });
    expect(refreshed.result.current.params.since).toEqual(inboxPresetDate("today"));
    refreshed.rerender({ scope: "all", base: all, accounts: [] });
    act(() => refreshed.result.current.setParams((previous) => ({ ...previous, since: inboxPresetDate("week"), datePreset: "week" })));
    expect(JSON.parse(sessionStorage.getItem(key)!).value.filters).toEqual({ datePreset: "week" });
    refreshed.unmount();
    vi.setSystemTime(new Date("2026-10-10T12:00:00Z"));
    expect(mount().result.current.params.since).toEqual(inboxPresetDate("week"));
  });

  it("remembers actual clearing and restores the receiving workspace synchronously", () => {
    const page = mount();
    act(() => { page.result.current.setParams((previous) => ({ ...previous, from: "sender", categoryIds: ["removed-label"] })); page.result.current.setSearch("first"); });
    act(() => useAppStore.setState({ currentOrganization: { id: "other", name: "Other", role: "owner" } }));
    expect(page.result.current.search).toBe("");
    expect(page.result.current.params.from).toBeUndefined();
    act(() => page.result.current.setSearch("second"));
    act(() => useAppStore.setState({ currentOrganization: { id: "workspace", name: "Workspace", role: "owner" } }));
    expect(page.result.current.search).toBe("first");
    expect(page.result.current.params.categoryIds).toEqual(["removed-label"]);
    act(() => { page.result.current.setParams(page.result.current.baseParams); page.result.current.setSearch(""); });
    page.unmount();
    expect(mount().result.current.params.from).toBeUndefined();
    expect(mount().result.current.search).toBe("");
  });

  it.each([{ since: null }, { until: "2026-02-31T00:00:00Z" }, { since: 0 }, { unseen: "false" }, { accountIds: [""] }, { datePreset: "stale" }, { cursor: "forbidden" }])("rejects stale or invalid persisted filter shapes: %j", (filters) => {
    const value = { scope: "all", search: "must-not-restore", sortBy: "newest", filters };
    expect(inboxListSchema.safeParse(value).success).toBe(false);
    sessionStorage.setItem(key, JSON.stringify({ value }));
    expect(mount().result.current.search).toBe("");
  });

  it("deduplicates ids, rejects invalid dates and stale sorts", () => {
    const value = { scope: "all", search: "", sortBy: "newest", filters: { categoryIds: ["a", "a"] } };
    expect(inboxListSchema.parse(value).filters.categoryIds).toEqual(["a"]);
    expect(inboxListSchema.safeParse({ ...value, sortBy: "unknown" }).success).toBe(false);
    expect(inboxListSchema.safeParse({ ...value, filters: { since: new Date(NaN) } }).success).toBe(false);
  });

  it("initializes debounce mirrors from restored values and switches resources without a stale query", () => {
    const page = renderHook(({ value, resource }) => useInboxDebouncedValue(value, resource), { wrapper: Owner, initialProps: { value: "restored", resource: "first" } });
    expect(page.result.current).toBe("restored");
    page.rerender({ value: "receiving", resource: "second" });
    expect(page.result.current).toBe("receiving");
    act(() => useAppStore.setState({ currentOrganization: { id: "other", name: "Other", role: "owner" } }));
    expect(page.result.current).toBe("receiving");
  });
});
