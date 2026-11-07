import { describe, expect, it } from "vitest";
import { mergeHead } from "./refreshListHead";
import type { UniboxListRow } from "@/lib/api/client/app/unibox/searchIncoming";

const row = (thread: string, minutesAgo: number): UniboxListRow => ({
    id: `00000000-0000-4000-8000-${String(minutesAgo).padStart(12, "0")}`,
    email_id: "box",
    thread_id: thread,
    from_addr: [],
    to_addr: [],
    subject: thread,
    snippet: "",
    internal_date: new Date(Date.UTC(2026, 10, 7, 12, 0) - minutesAgo * 60_000),
    seen: true,
    message_count: 1,
    has_unread: false,
    labels: [],
});

const page = (rows: UniboxListRow[], next: string | null) => ({
    data: rows,
    pagination: { has_more: next !== null, next_cursor: next },
});

const threads = (data: ReturnType<typeof mergeHead>) => data?.pages.map((p) => p.data.map((r) => r.thread_id));

describe("mergeHead", () => {
    const old = {
        pages: [page([row("a", 1), row("b", 2), row("c", 3)], "c"), page([row("d", 4), row("e", 5)], "e")],
        pageParams: [null, "c"],
    };

    it("puts new arrivals on top, keeps rows pushed past the head and the later pages", () => {
        const fresh = page([row("new", 0), row("a", 1), row("b", 2)], "b");
        const merged = mergeHead(old, fresh);
        expect(threads(merged)).toEqual([["new", "a", "b", "c"], ["d", "e"]]);
        expect(merged?.pages[0].pagination.next_cursor).toBe("c");
        expect(merged?.pageParams).toEqual([null, "c"]);
    });

    it("drops a head row that left the list and moves a revived thread out of a later page", () => {
        const fresh = page([row("d", 0), row("a", 1), row("c", 3)], "c");
        expect(threads(mergeHead(old, fresh))).toEqual([["d", "a", "c"], ["e"]]);
    });

    it("collapses to one page when everything now fits in it", () => {
        const fresh = page([row("a", 1), row("e", 5)], null);
        const merged = mergeHead(old, fresh);
        expect(threads(merged)).toEqual([["a", "e"]]);
        expect(merged?.pageParams).toEqual([null]);
    });
});
