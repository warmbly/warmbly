import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { UniboxSearchParams } from "@/lib/api/models/app/unibox/UniboxSearch";
import useUniboxSearch from "./useUniboxSearch";

vi.mock("@/lib/api/client/app/unibox/searchIncoming", () => ({
    default: () => new Promise(() => {}),
}));

describe("unibox scope transitions", () => {
    it.each<{
        name: string; initial: UniboxSearchParams; next: UniboxSearchParams;
        scope: string; nextScope: string; keep?: boolean;
    }>([
        { name: "Sent to Inbox", scope: "folder:sent", nextScope: "folder:inbox", initial: { folder: "sent" }, next: { folder: "inbox" } },
        { name: "All mail to Inbox", scope: "all", nextScope: "folder:inbox", initial: {}, next: { folder: "inbox" } },
        { name: "All mail to Unread", scope: "all", nextScope: "unread", initial: {}, next: { unseen: true } },
        { name: "mailbox change", scope: "mailbox:a", nextScope: "mailbox:b", initial: { accountIds: ["a"] }, next: { accountIds: ["b"] } },
        { name: "history recipient change", scope: "history:a:all", nextScope: "history:b:all", initial: { address: "a" }, next: { address: "b" } },
        { name: "search within Inbox", scope: "folder:inbox", nextScope: "folder:inbox", initial: { folder: "inbox" }, next: { folder: "inbox", query: "hello" }, keep: true },
        { name: "filter within All mail", scope: "all", nextScope: "all", initial: {}, next: { unseen: true }, keep: true },
    ])("keeps previous rows only within the same scope: $name", ({ initial, next, scope, nextScope, keep = false }) => {
        const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
        const row = {
            id: "sent-message", email_id: "mailbox", thread_id: "outbound-thread",
            from_addr: ["me@example.com"], to_addr: ["client@example.com"],
            subject: "Sent message", snippet: "Hello", internal_date: "2026-09-16T00:00:00Z",
            seen: true, message_count: 1, has_unread: false, labels: [],
        };
        client.setQueryData(["unibox", "search", initial, scope], {
            pages: [{ data: [row], pagination: { has_more: false, next_cursor: null } }],
            pageParams: [null],
        });
        const wrapper = ({ children }: { children: ReactNode }) => (
            <QueryClientProvider client={client}>{children}</QueryClientProvider>
        );
        const { result, rerender, unmount } = renderHook(
            ({ params, scope }: { params: UniboxSearchParams; scope: string }) => useUniboxSearch(params, scope),
            { initialProps: { params: initial, scope }, wrapper },
        );
        expect(result.current.emails).toEqual([row]);
        rerender({ params: next, scope: nextScope });
        expect(result.current.emails).toEqual(keep ? [row] : []);
        expect(result.current.isPending).toBe(!keep);
        unmount();
        client.clear();
    });
});
