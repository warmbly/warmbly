import { cleanup, render, screen } from "@testing-library/react";
import { createMemoryHistory, createRootRoute, createRouter, RouterProvider } from "@tanstack/react-router";
import { afterEach, expect, it, vi } from "vitest";
import CloudPoolBanner from "./CloudPoolBanner";

const fixture = vi.hoisted(() => ({ unavailable: false, mailboxes: Array.from({ length: 8 }, (_, i) => ({ id: `mailbox-${i}`, status: "active" })) }));
vi.mock("@/lib/api/hooks/app/emails/useEmails", () => ({ default: () => ({ emails: fixture.mailboxes }) }));
vi.mock("@/hooks/useCloudPool", () => ({ default: () => ({ manageable: true, connected: true, unavailable: fixture.unavailable, workspaceConnected: true, enrolledCount: 9,
    plan: { enrolled: 10, warming: 10, mailbox_limit: 10 },
    rowFor: (id: string) => ({ id, enrolled: true, cloud: { status: "active", warmup: { enabled: true, paused: Number(id.split("-")[1]) >= 6 } } }),
}) }));
afterEach(cleanup);

it.each([false, true])("separates inventory counts from quota and preserves positive reports during a connection error (%s)", async (unavailable) => {
    fixture.unavailable = unavailable;
    const router = createRouter({ routeTree: createRootRoute({ component: () => <CloudPoolBanner onConnect={() => {}} /> }), history: createMemoryHistory() });
    await router.load();
    render(<RouterProvider router={router} />);
    expect(await screen.findByText("8 of 8 mailboxes connected to Warmbly Cloud. 6 warming.")).toBeInTheDocument();
    expect(screen.getByText("10 of 10 free.")).toBeInTheDocument();
});
