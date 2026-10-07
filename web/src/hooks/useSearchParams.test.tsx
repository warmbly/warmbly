import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, Outlet, RouterProvider } from "@tanstack/react-router";
import { describe, expect, it } from "vitest";
import { parseSearch, stringifySearch } from "@/lib/routerSearch";
import { useSearchParam, useSearchParams } from "./useSearchParams";

function Filters() {
    const [tag, setTag] = useSearchParam("tag");
    const [, setParams] = useSearchParams();
    return <>
        <output aria-label="Tag filter">{tag || "All accounts"}</output>
        <button onClick={() => setTag("sending-tag")}>Sending Account</button>
        <button onClick={() => setTag("")}>All accounts</button>
        <button onClick={() => setParams((prev) => {
            const next = new URLSearchParams(prev);
            next.delete("mailbox");
            next.delete("tab");
            return next;
        }, { replace: true })}>Consume mailbox link</button>
    </>;
}

async function open(href: string) {
    const root = createRootRoute({ component: Outlet });
    const route = createRoute({ getParentRoute: () => root, path: "/app/emails", component: Filters });
    const history = createMemoryHistory({ initialEntries: [href] });
    const router = createRouter({ routeTree: root.addChildren([route]), history, parseSearch, stringifySearch });
    await act(() => router.load());
    return { router, ...render(<RouterProvider router={router} />) };
}

describe("URL filter selection", () => {
    it("keeps a selection through a reload without discarding mailbox links", async () => {
        const page = await open("/app/emails?mailbox=mailbox-id&tab=settings");
        expect(screen.getByLabelText("Tag filter")).toHaveTextContent("All accounts");
        fireEvent.click(screen.getByRole("button", { name: "Sending Account" }));
        await waitFor(() => expect(page.router.state.location.searchStr).toContain("tag=sending-tag"));
        const href = page.router.state.location.href;
        expect(href).toContain("mailbox=mailbox-id");
        expect(href).toContain("tab=settings");
        page.unmount();

        const reloaded = await open(href);
        expect(screen.getByLabelText("Tag filter")).toHaveTextContent("sending-tag");
        fireEvent.click(screen.getByRole("button", { name: "Consume mailbox link" }));
        await waitFor(() => expect(reloaded.router.state.location.searchStr).toBe("?tag=sending-tag"));
        expect(screen.getByLabelText("Tag filter")).toHaveTextContent("sending-tag");
    });

    it("clears only the chosen filter and preserves unrelated search parameters", async () => {
        const page = await open("/app/emails?tag=sending-tag&mailbox=mailbox-id");
        fireEvent.click(screen.getByRole("button", { name: "All accounts" }));
        await waitFor(() => expect(page.router.state.location.searchStr).toBe("?mailbox=mailbox-id"));
        expect(screen.getByLabelText("Tag filter")).toHaveTextContent("All accounts");
        const href = page.router.state.location.href;
        page.unmount();
        await open(href);
        expect(screen.getByLabelText("Tag filter")).toHaveTextContent("All accounts");
    });
});
