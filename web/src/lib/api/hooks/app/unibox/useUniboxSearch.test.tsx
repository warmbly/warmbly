import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { UniboxSearchParams } from "@/lib/api/models/app/unibox/UniboxSearch";
import useUniboxSearch from "./useUniboxSearch";

vi.mock("@/lib/api/client/app/unibox/searchIncoming", () => ({
    default: () => new Promise(() => {}),
}));

describe("unibox folder transitions", () => {
    it.each(["sent", undefined] as const)("does not show %s mail while Inbox loads", (folder) => {
        const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
        const initial: UniboxSearchParams = { folder };
        const row = {
            id: "sent-message", email_id: "mailbox", thread_id: "outbound-thread",
            from_addr: ["me@example.com"], to_addr: ["client@example.com"],
            subject: "Sent message", snippet: "Hello", internal_date: "2026-09-16T00:00:00Z",
            seen: true, message_count: 1, has_unread: false, labels: [],
        };
        client.setQueryData(["unibox", "search", initial], {
            pages: [{ data: [row], pagination: { has_more: false, next_cursor: null } }],
            pageParams: [null],
        });
        const wrapper = ({ children }: { children: ReactNode }) => (
            <QueryClientProvider client={client}>{children}</QueryClientProvider>
        );
        const { result, rerender, unmount } = renderHook(
            (params: UniboxSearchParams) => useUniboxSearch(params),
            { initialProps: initial, wrapper },
        );
        expect(result.current.emails).toEqual([row]);
        rerender({ folder: "inbox" });
        expect(result.current.emails).toEqual([]);
        expect(result.current.isPending).toBe(true);
        unmount();
        client.clear();
    });
});
