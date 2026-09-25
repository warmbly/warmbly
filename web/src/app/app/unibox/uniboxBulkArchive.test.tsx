// Select all, archive, and again: every round has to leave the list for good.
//
// The fake server below keeps real folder state and pages like the backend, so
// what the list shows after each round is what a reload would show. The rows
// are counted in the DOM, including any still mounted after their exit, which
// is how a stalled exit shows up: the archived rows stay on screen.

import React from "react";
import { describe, it, expect, vi, beforeAll, beforeEach } from "vitest";
import { screen, act, fireEvent, within } from "@testing-library/react";
import { installLayoutShims, mount, setViewportWidth, settle, SUITE } from "./uniboxHarness";

type Cfg = { method?: string; url?: string; data?: Record<string, unknown> };

const server = vi.hoisted(() => {
    const rows = Array.from({ length: 130 }, (_, i) => ({
        id: `msg-${i}`,
        email_id: "mbox-1",
        thread_id: `thread-${i}`,
        from_addr: [`Sender ${i} <s${i}@example.com>`],
        to_addr: ["me@warmbly.com"],
        subject: `Subject ${i}`,
        snippet: `Snippet ${i}`,
        internal_date: new Date(Date.now() - i * 60e3).toISOString(),
        seen: true,
        message_count: 1,
        has_unread: false,
        labels: [],
        folder: "inbox",
    }));
    return { rows, failNext: false };
});

beforeAll(() => {
    installLayoutShims();
    setViewportWidth(1512);
});

vi.mock("@/lib/api/client/Request", () => ({
    default: async (cfg: Cfg) => {
        const url = String(cfg.url ?? "");
        await new Promise((r) => setTimeout(r, 30));
        if (cfg.method === "PATCH" && url === "/unibox/folder") {
            if (server.failNext) {
                server.failNext = false;
                throw new Error("unavailable");
            }
            const ids = (cfg.data?.thread_ids as string[]) ?? [];
            for (const r of server.rows) if (ids.includes(r.thread_id)) r.folder = String(cfg.data?.folder);
            return undefined;
        }
        if (url.startsWith("/unibox?")) {
            const p = new URL(url, "https://t.local").searchParams;
            const folder = p.get("folder");
            const limit = Number(p.get("limit") ?? 50);
            let list = server.rows.filter((r) => !folder || r.folder === folder);
            const cursor = p.get("cursor");
            if (cursor) list = list.slice(list.findIndex((r) => r.id === cursor) + 1);
            const page = list.slice(0, limit);
            const more = list.length > limit;
            return {
                data: page,
                pagination: { has_more: more, next_cursor: more ? page[page.length - 1].id : null },
            };
        }
        const { route } = await import("./uniboxHarness");
        return route(url);
    },
}));
vi.mock("@/lib/helper/getToken", () => ({
    default: () => ({
        access_token: "a",
        refresh_token: "r",
        access_token_expires_at: new Date(Date.now() + 3600e3).toISOString(),
        refresh_token_expires_at: new Date(Date.now() + 3600e3).toISOString(),
    }),
}));
vi.mock("@/hooks/SocketProvider", () => ({
    default: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));
vi.mock("@/hooks/context/socket", async (orig) => {
    const actual = (await orig()) as Record<string, unknown>;
    return {
        ...actual,
        useSocket: () => ({ isConnected: false, subscribeToChannel: () => () => {}, pushToChannel: () => {}, socket: null, status: "closed" }),
        useChannel: () => ({ state: "closed", push: () => {}, channel: null }),
        useChannelEvent: () => {},
        useChannelSubscription: () => {},
    };
});

const visible = () =>
    [...document.querySelectorAll("[data-thread-id]")].map((e) => e.getAttribute("data-thread-id"));

async function archiveAll() {
    const select = screen.queryByRole("button", { name: "Select" });
    if (select) await act(async () => { fireEvent.click(select); });
    await settle();
    await act(async () => { fireEvent.click(screen.getByLabelText("Select all loaded")); });
    await settle();
    const bar = screen.getByRole("toolbar", { name: "Selection actions" });
    await act(async () => { fireEvent.click(within(bar).getByTitle("Archive")); });
    for (let i = 0; i < 6; i++) await settle();
}

describe("bulk archive from the selection bar", SUITE, () => {
    beforeEach(() => {
        for (const r of server.rows) r.folder = "inbox";
        server.failNext = false;
    });

    it("clears each round of select all for good", async () => {
        await mount("/app/unibox/inbox");
        for (let i = 0; i < 4; i++) await settle();
        const first = visible();
        expect(first).toHaveLength(50);

        await archiveAll();
        const second = visible();
        // The next fifty, and nothing from the first round left mounted.
        expect(second).toHaveLength(50);
        expect(second.filter((id) => first.includes(id))).toEqual([]);

        await archiveAll();
        const third = visible();
        expect(third).toHaveLength(30);
        expect(third.filter((id) => first.includes(id) || second.includes(id))).toEqual([]);

        const archived = server.rows.filter((r) => r.folder === "archive").length;
        expect(archived).toBe(100);
    });

    it("puts a failed round back exactly once", async () => {
        await mount("/app/unibox/inbox");
        for (let i = 0; i < 4; i++) await settle();
        const before = visible();

        server.failNext = true;
        await archiveAll();
        const after = visible();
        expect(after).toHaveLength(before.length);
        expect(new Set(after).size).toBe(after.length);
        expect([...after].sort()).toEqual([...before].sort());
    });
});
