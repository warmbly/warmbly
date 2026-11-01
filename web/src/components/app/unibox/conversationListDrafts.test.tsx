// The Drafts folder lists threads with a pending inbox-agent draft above the mailbox's own drafts.

import React from "react";
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { UniboxSearchParams } from "@/lib/api/models/app/unibox/UniboxSearch";
import { ConversationList } from "./ConversationList";

const search = vi.hoisted(() => ({
  calls: [] as { params: UniboxSearchParams; enabled: boolean }[],
  agentHasMore: false,
  agentPageFailed: false,
  mailboxHasMore: false,
  agentNextPage: [] as string[],
  agentPending: false,
  agentFailed: false,
  agentRefetches: 0,
  mailboxEmpty: false,
}));

function row(id: string, thread: string, subject: string) {
  return {
    id,
    thread_id: thread,
    email_id: "m1",
    from_addr: ["Lead <lead@example.com>"],
    to_addr: ["me@example.com"],
    subject,
    snippet: subject,
    internal_date: new Date().toISOString(),
    has_unread: false,
    message_count: 1,
    labels: [],
  };
}

vi.mock("@/lib/api/hooks/app/unibox/useUniboxSearch", () => ({
  default: (params: UniboxSearchParams, _scopeKey: string, enabled = true) => {
    search.calls.push({ params, enabled });
    const emails = !enabled
      ? []
      : params.agentDrafts
        ? search.agentPending || search.agentFailed ? [] : [row("a1", "t-agent", "Agent reply to a lead"), row("a2", "t-both", "Agent reply on a drafted thread")]
        : search.mailboxEmpty ? [] : [row("d1", "t-mine", "My own draft"), row("d2", "t-both", "Provider copy of a drafted thread")];
    return {
      emails,
      isPending: !!params.agentDrafts && search.agentPending,
      isFetching: false,
      isPlaceholderData: false,
      isError: !!params.agentDrafts && search.agentFailed,
      error: search.agentFailed ? new Error("Draft request failed") : null,
      hasNextPage: enabled && (params.agentDrafts ? search.agentHasMore : search.mailboxHasMore),
      isFetchingNextPage: false,
      isFetchNextPageError: !!params.agentDrafts && search.agentPageFailed,
      fetchNextPage: () => {
        search.agentNextPage.push(params.agentDrafts ? "agent" : "mailbox");
        return Promise.resolve();
      },
      refetch: () => {
        if (params.agentDrafts) search.agentRefetches++;
        return Promise.resolve();
      },
    };
  },
}));
vi.mock("./ConversationItem", () => ({
  ConversationItem: ({ email }: { email: { subject: string } }) => <div data-testid="row">{email.subject}</div>,
}));
vi.mock("./AnimatedRow", () => ({
  AnimatedRow: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));
vi.mock("./SelectionBar", () => ({ SelectionBar: () => null }));
vi.mock("./UniboxFilterPopover", () => ({
  countUserFilters: () => 0,
  UniboxFilterButton: () => null,
  UniboxFilterChips: () => null,
}));
vi.mock("@/components/app/unibox/compose/ComposeButton", () => ({ default: () => null }));
vi.mock("@/hooks/useConversationActions", () => ({ useConversationActions: () => ({}) }));
vi.mock("@/hooks/useShortcutActions", () => ({ useShortcutActions: () => {} }));
vi.mock("@/hooks/usePermission", () => ({ useAccessRestricted: () => false }));
vi.mock("@/hooks/useBrowseDebouncedValue", () => ({ default: (v: unknown) => v }));
vi.mock("@/hooks/useScrollMemory", () => ({ useScrollMemory: () => {} }));
vi.mock("framer-motion", async (importOriginal) => ({
  ...(await importOriginal<Record<string, unknown>>()),
  AnimatePresence: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

afterEach(() => {
  cleanup();
  search.calls = [];
  search.agentHasMore = false;
  search.agentPageFailed = false;
  search.mailboxHasMore = false;
  search.agentNextPage = [];
  search.agentPending = false;
  search.agentFailed = false;
  search.agentRefetches = 0;
  search.mailboxEmpty = false;
});

function mount(params: UniboxSearchParams, scopeKey: string) {
  return render(
    <ConversationList
      scopeKey={scopeKey}
      scopeLabel="Drafts"
      params={params}
      baseParams={params}
      setParams={() => {}}
      search=""
      setSearch={() => {}}
    />,
  );
}

it("lists pending agent drafts first under their own header, then the mailbox drafts", () => {
  mount({ sortBy: "newest", folder: "drafts" }, "folder:drafts");

  expect(screen.getByText("Agent drafts")).toBeTruthy();
  expect(screen.getAllByTestId("row").map((r) => r.textContent)).toEqual([
    "Agent reply to a lead",
    "Agent reply on a drafted thread",
    "My own draft",
  ]);
  const agent = search.calls.find((c) => c.params.agentDrafts);
  expect(agent?.enabled).toBe(true);
  expect(agent?.params.folder).toBeUndefined();
});

it("leaves every other folder alone", () => {
  mount({ sortBy: "newest", folder: "inbox", automated: false }, "folder:inbox");

  expect(screen.queryByText("Agent drafts")).toBeNull();
  expect(search.calls.filter((c) => c.params.agentDrafts).every((c) => !c.enabled)).toBe(true);
  expect(screen.getAllByTestId("row")).toHaveLength(2);
});

it("pages pending agent drafts through Load more without eagerly fetching every page", () => {
  search.agentHasMore = true;
  mount({ sortBy: "newest", folder: "drafts" }, "folder:drafts");

  expect(search.agentNextPage).toEqual([]);
  fireEvent.click(screen.getByRole("button", { name: "Load more" }));
  expect(search.agentNextPage).toEqual(["agent"]);
  expect(search.agentNextPage).not.toContain("mailbox");
});

it("does not claim Drafts is empty while agent drafts are still loading", () => {
  search.agentPending = true;
  search.mailboxEmpty = true;
  const { container } = mount({ sortBy: "newest", folder: "drafts" }, "folder:drafts");

  expect(container.querySelector(".animate-pulse")).toBeTruthy();
  expect(screen.queryByText(/No conversations/)).toBeNull();
});

it("offers a retry when agent drafts fail while keeping mailbox drafts visible", () => {
  search.agentFailed = true;
  mount({ sortBy: "newest", folder: "drafts" }, "folder:drafts");

  expect(screen.getByRole("alert").textContent).toContain("couldn't load");
  expect(screen.getByText("My own draft")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Try again" }));
  expect(search.agentRefetches).toBe(1);
});

it("shows a retry instead of an empty folder when the agent request fails", () => {
  search.agentFailed = true;
  search.mailboxEmpty = true;
  mount({ sortBy: "newest", folder: "drafts" }, "folder:drafts");

  expect(screen.getByText("Couldn't load inbox")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Try again" }));
  expect(search.agentRefetches).toBe(1);
});

it("preserves mailbox, category and search filters for agent drafts", () => {
  mount({ sortBy: "newest", folder: "drafts", accountIds: ["mailbox"], categoryIds: ["category"], query: "proposal" }, "folder:drafts");

  expect(search.calls.find((c) => c.params.agentDrafts)?.params).toMatchObject({
    accountIds: ["mailbox"], categoryIds: ["category"], query: "proposal",
  });
});

it("keeps an explicitly empty mailbox scope empty", () => {
  mount({ sortBy: "newest", folder: "drafts", accountIds: [] }, "folder:drafts");

  expect(search.calls.every((c) => !c.enabled)).toBe(true);
  expect(screen.queryAllByTestId("row")).toHaveLength(0);
});

it("never pages agent drafts outside the Drafts folder", () => {
  search.agentHasMore = true;
  mount({ sortBy: "newest", folder: "inbox", automated: false }, "folder:inbox");

  expect(search.agentNextPage).toEqual([]);
});

it("keeps mailbox pagination and a separate agent retry reachable after an agent-page failure", () => {
  search.agentHasMore = true;
  search.agentPageFailed = true;
  search.mailboxHasMore = true;
  mount({ sortBy: "newest", folder: "drafts" }, "folder:drafts");

  fireEvent.click(screen.getByRole("button", { name: "Load more" }));
  expect(search.agentNextPage).toEqual(["mailbox"]);
  fireEvent.click(screen.getByRole("button", { name: "Retry agent drafts" }));
  expect(search.agentNextPage).toEqual(["mailbox", "agent"]);
});
