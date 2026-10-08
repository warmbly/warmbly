import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, Outlet, RouterProvider } from "@tanstack/react-router";
import { beforeEach, describe, expect, it } from "vitest";
import { parseSearch, stringifySearch } from "@/lib/routerSearch";
import useMailboxTagFilter from "./useMailboxTagFilter";

let workspace = "workspace";
let tags: readonly { id: string }[] | undefined;

function Filters() {
    const [tag, setTag] = useMailboxTagFilter(`user:${workspace}`, tags);
    return <><output aria-label="Tag">{tag || "All accounts"}</output>
        <button onClick={() => setTag("sending")}>Sending</button>
        <button onClick={() => setTag("")}>All accounts</button></>;
}

async function open(href = "/app/emails") {
    const root = createRootRoute({ component: Outlet });
    const emails = createRoute({ getParentRoute: () => root, path: "/app/emails", component: Filters });
    const tasks = createRoute({ getParentRoute: () => root, path: "/app/tasks", component: () => <div>Tasks</div> });
    const router = createRouter({ routeTree: root.addChildren([emails, tasks]), history: createMemoryHistory({ initialEntries: [href] }), parseSearch, stringifySearch });
    await act(() => router.load());
    return { router, ...render(<RouterProvider router={router} />) };
}

describe("remembered mailbox tag", () => {
    beforeEach(() => {
        sessionStorage.clear();
        workspace = "workspace";
        tags = [{ id: "sending" }];
    });
    it("waits for profile tags and keeps other workspaces isolated", async () => {
        tags = undefined;
        sessionStorage.setItem("warmbly:mailbox-tag:user:workspace", "sending");
        const page = await open();
        expect(screen.getByLabelText("Tag")).toHaveTextContent("sending");
        tags = [{ id: "sending" }];
        page.rerender(<RouterProvider router={page.router} />);
        expect(screen.getByLabelText("Tag")).toHaveTextContent("sending");
        page.unmount();
        workspace = "another-workspace";
        await open();
        expect(screen.getByLabelText("Tag")).toHaveTextContent("All accounts");
        expect(sessionStorage.getItem("warmbly:mailbox-tag:user:workspace")).toBe("sending");
    });
    it("survives navigating to Tasks and returning through a queryless Accounts link", async () => {
        const page = await open();
        fireEvent.click(screen.getByRole("button", { name: "Sending" }));
        await waitFor(() => expect(page.router.state.location.searchStr).toBe("?tag=sending"));
        await act(() => page.router.navigate({ href: "/app/tasks" }));
        await act(() => page.router.navigate({ to: "/app/emails", search: { mailbox: "mailbox", tab: "settings" } }));
        await waitFor(() => expect(screen.getByLabelText("Tag")).toHaveTextContent("sending"));
        expect(page.router.state.location.searchStr).toContain("mailbox=mailbox");
        expect(page.router.state.location.searchStr).toContain("tab=settings");
        const href = page.router.state.location.href;
        page.unmount();
        await open(href);
        expect(screen.getByLabelText("Tag")).toHaveTextContent("sending");
    });
    it("keeps All accounts after clearing and falls back for deleted or other-workspace tags", async () => {
        sessionStorage.setItem("warmbly:mailbox-tag:other:workspace", "sending");
        const page = await open("/app/emails?tag=sending");
        fireEvent.click(screen.getByRole("button", { name: "All accounts" }));
        await waitFor(() => expect(page.router.state.location.searchStr).toBe(""));
        await act(() => page.router.navigate({ href: "/app/tasks" }));
        await act(() => page.router.navigate({ to: "/app/emails" }));
        expect(screen.getByLabelText("Tag")).toHaveTextContent("All accounts");
        page.unmount();
        await open("/app/emails?tag=deleted");
        await waitFor(() => expect(screen.getByLabelText("Tag")).toHaveTextContent("All accounts"));
    });
});
