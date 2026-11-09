import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { FormSubmissionsPage, FormSubmissionsPageParam } from "@/lib/api/client/app/forms";
import type { FormSubmission } from "@/lib/api/models/app/forms/Form";
import { compareSubmissions } from "./submissionsWindow";

// A keyset server: newest first by (created_at, id), cursor = the last row's
// position, so a deleted cursor row still pages correctly.
const server = vi.hoisted(() => ({ rows: [] as { id: string; created_at: string }[], fail: false }));

vi.mock("@/lib/api/client/app/forms", async (orig) => ({
    ...(await orig<Record<string, unknown>>()),
    listFormSubmissions: async (_id: string, limit: number, page?: FormSubmissionsPageParam): Promise<FormSubmissionsPage> => {
        const key = (r: { id: string; created_at: string }) => `${r.created_at}|${r.id}`;
        const sorted = [...server.rows].sort((a, b) => (key(a) < key(b) ? 1 : -1));
        const after = page && "cursor" in page ? page.cursor : null;
        const rest = after ? sorted.filter((r) => key(r) < after) : sorted;
        const data = rest.slice(0, limit).map((r) => sub(r.id, r.created_at));
        const hasMore = rest.length > limit;
        return { data, has_more: hasMore, next: hasMore ? { cursor: key(data[data.length - 1]) } : null };
    },
    deleteFormSubmission: async (_form: string, id: string) => {
        if (server.fail) throw new Error("refused");
        server.rows = server.rows.filter((r) => r.id !== id);
    },
}));

import { useDeleteFormSubmission, useFormSubmissions, useLoadMoreFormSubmissions } from "./index";

function sub(id: string, created_at: string): FormSubmission {
    return { id, created_at, form_id: "form", organization_id: "org", data: {}, source_url: "" };
}

// Fixed-width microsecond stamps, so the fake server can order them as strings.
const at = (us: number) => `2026-03-01T12:00:00.${String(us).padStart(6, "0")}Z`;

function seed(n: number) {
    // Pairs share a microsecond so page boundaries fall inside ties.
    server.rows = Array.from({ length: n }, (_, i) => ({ id: `s${String(i).padStart(3, "0")}`, created_at: at(100 + Math.floor(i / 2)) }));
}

function setup() {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>;
    const hook = renderHook(
        () => ({ list: useFormSubmissions("form"), more: useLoadMoreFormSubmissions("form"), remove: useDeleteFormSubmission() }),
        { wrapper },
    );
    const ids = () => hook.result.current.list.data?.rows.map((r) => r.id) ?? [];
    const refresh = async () => {
        await act(() => client.invalidateQueries({ queryKey: ["forms"] }));
        await waitFor(() => expect(hook.result.current.list.isFetching).toBe(false));
    };
    return { hook, ids, refresh };
}

const serverOrder = () => [...server.rows].map((r) => sub(r.id, r.created_at)).sort(compareSubmissions).map((r) => r.id);

describe("loaded form submissions under realtime refreshes", () => {
    beforeEach(() => {
        seed(120);
        server.fail = false;
    });

    it("keeps every loaded page when new submissions push the first page down", async () => {
        const { hook, ids, refresh } = setup();
        await waitFor(() => expect(ids()).toHaveLength(50));
        await act(() => hook.result.current.more.mutateAsync());
        await waitFor(() => expect(ids()).toHaveLength(100));
        expect(ids()).toEqual(serverOrder().slice(0, 100));
        const loaded = ids();

        server.rows.push(...["n1", "n2", "n3"].map((id, i) => ({ id, created_at: at(900 + i) })));
        await refresh();

        expect(ids().slice(0, 3)).toEqual(["n3", "n2", "n1"]);
        expect(ids()).toHaveLength(103);
        for (const id of loaded) expect(ids()).toContain(id);
        expect(new Set(ids()).size).toBe(ids().length);
        expect(ids()).toEqual(serverOrder().slice(0, ids().length));

        // Loading on continues from the oldest loaded row with nothing skipped or repeated.
        while (hook.result.current.list.data?.hasMore) await act(() => hook.result.current.more.mutateAsync());
        expect(ids()).toEqual(serverOrder());
    });

    it("drops a submission deleted elsewhere from a later page, including the oldest loaded row", async () => {
        const { hook, ids, refresh } = setup();
        await waitFor(() => expect(ids()).toHaveLength(50));
        await act(() => hook.result.current.more.mutateAsync());
        await waitFor(() => expect(ids()).toHaveLength(100));
        const loaded = ids();
        const gone = new Set([loaded[70], loaded[loaded.length - 1]]);

        server.rows = server.rows.filter((r) => !gone.has(r.id));
        await refresh();

        for (const id of gone) expect(ids()).not.toContain(id);
        // The refresh reads as many rows as were loaded, so it reaches two past the old floor.
        expect(ids()).toEqual(serverOrder().slice(0, 100));
        for (const id of loaded.filter((id) => !gone.has(id))) expect(ids()).toContain(id);
        while (hook.result.current.list.data?.hasMore) await act(() => hook.result.current.more.mutateAsync());
        expect(ids()).toEqual(serverOrder());
    });

    it("removes a deleted row from every loaded page at once and restores it when the server refuses", async () => {
        const { hook, ids } = setup();
        await waitFor(() => expect(ids()).toHaveLength(50));
        await act(() => hook.result.current.more.mutateAsync());
        await waitFor(() => expect(ids()).toHaveLength(100));
        const target = ids()[80];

        server.fail = true;
        await act(async () => {
            await hook.result.current.remove.mutateAsync({ formId: "form", submissionId: target }).catch(() => {});
        });
        await waitFor(() => expect(hook.result.current.list.isFetching).toBe(false));
        expect(ids()).toContain(target);

        server.fail = false;
        let pending: Promise<void> | undefined;
        act(() => {
            pending = hook.result.current.remove.mutateAsync({ formId: "form", submissionId: target });
        });
        await waitFor(() => expect(ids()).not.toContain(target));
        await act(() => pending);
        await waitFor(() => expect(hook.result.current.list.isFetching).toBe(false));
        expect(ids()).not.toContain(target);
        expect(ids()).toHaveLength(99);
    });
});

describe("compareSubmissions", () => {
    it("orders by microseconds Date cannot see, then by id", () => {
        const rows = [
            sub("a", "2026-03-01T12:00:00.123456Z"),
            sub("b", "2026-03-01T12:00:00.123457Z"),
            sub("c", "2026-03-01T12:00:00.123456Z"),
            sub("d", "2026-03-01T13:00:00.1234565+01:00"),
            sub("e", "2026-03-01T12:00:00Z"),
        ];
        expect([...rows].sort(compareSubmissions).map((r) => r.id)).toEqual(["b", "d", "c", "a", "e"]);
    });
});
