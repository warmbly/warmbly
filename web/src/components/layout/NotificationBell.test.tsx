import type { PropsWithChildren } from "react";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AppNotification } from "@/lib/api/models/app/notifications/Notification";
import { NotificationBell } from "./NotificationBell";

const fixture = vi.hoisted(() => ({
    notifications: [] as AppNotification[],
    unread: 1,
    queries: vi.fn(),
    markAll: vi.fn(),
    markOne: vi.fn(),
}));

vi.mock("@tanstack/react-router", () => ({
    Link: ({ children, to, onClick }: PropsWithChildren<{ to: string; onClick?: () => void }>) => (
        <a href={to} onClick={onClick}>{children}</a>
    ),
}));
vi.mock("framer-motion", async (importOriginal) => ({
    ...(await importOriginal<Record<string, unknown>>()),
    AnimatePresence: ({ children }: PropsWithChildren) => children,
}));
vi.mock("@/lib/api/hooks/app/notifications/useNotifications", () => ({
    useNotifications: (unreadOnly: boolean) => {
        fixture.queries(unreadOnly);
        return {
            data: { notifications: unreadOnly ? fixture.notifications.filter((n) => !n.read_at) : fixture.notifications, unread: fixture.unread },
            isLoading: false,
        };
    },
    useMarkAllNotificationsRead: () => ({ mutate: fixture.markAll }),
    useMarkNotificationRead: () => ({ mutate: fixture.markOne }),
}));

beforeEach(() => {
    vi.clearAllMocks();
    fixture.unread = 1;
    fixture.notifications = [
        { id: "new", user_id: "user", category: "inbound_reply", title: "New reply", created_at: new Date() },
        { id: "read", user_id: "user", category: "inbound_reply", title: "Already handled", created_at: new Date(), read_at: new Date() },
    ];
});
afterEach(cleanup);

describe("notification bell unread default", () => {
    it("portals the feed outside a clipped header with dialog semantics", () => {
        const { container } = render(<div style={{ overflow: "hidden", transform: "translateX(0)" }}><NotificationBell /></div>);
        fireEvent.click(screen.getByRole("button", { name: "Notifications" }));
        const feed = screen.getByRole("dialog", { name: "Notifications" });
        expect(container.contains(feed)).toBe(false);
        expect(feed.parentElement).toBe(document.body);
        expect(feed).toHaveStyle({ position: "fixed", visibility: "visible" });
        fireEvent.pointerDown(screen.getByRole("button", { name: "All" }));
        expect(feed).toBeInTheDocument();
    });

    it("requests and shows only unread notifications on the first opening", () => {
        render(<NotificationBell />);
        fireEvent.click(screen.getByRole("button", { name: "Notifications" }));
        expect(screen.getByRole("button", { name: "Unread", pressed: true })).toBeInTheDocument();
        expect(screen.getByText("New reply")).toBeInTheDocument();
        expect(screen.queryByText("Already handled")).not.toBeInTheDocument();
        expect(fixture.queries).toHaveBeenLastCalledWith(true);
    });

    it.each(["bell", "escape", "outside"])("resets to unread after closing All via %s", (close) => {
        render(<NotificationBell />);
        const bell = screen.getByRole("button", { name: "Notifications" });
        fireEvent.click(bell);
        fireEvent.click(screen.getByRole("button", { name: "All" }));
        expect(screen.getByText("Already handled")).toBeInTheDocument();
        expect(fixture.queries).toHaveBeenLastCalledWith(false);
        if (close === "bell") fireEvent.click(bell);
        else if (close === "escape") fireEvent.keyDown(document, { key: "Escape" });
        else fireEvent.pointerDown(document.body);
        fireEvent.click(bell);
        expect(screen.getByRole("button", { name: "Unread", pressed: true })).toBeInTheDocument();
        expect(screen.queryByText("Already handled")).not.toBeInTheDocument();
        expect(fixture.queries).toHaveBeenLastCalledWith(true);
    });

    it("shows caught up when only read history exists, without hiding All", () => {
        fixture.notifications = fixture.notifications.filter((n) => n.read_at);
        fixture.unread = 0;
        render(<NotificationBell />);
        fireEvent.click(screen.getByRole("button", { name: "Notifications" }));
        expect(screen.getByText("You're all caught up.")).toBeInTheDocument();
        expect(screen.queryByText("Already handled")).not.toBeInTheDocument();
        fireEvent.click(screen.getByRole("button", { name: "All" }));
        expect(screen.getByText("Already handled")).toBeInTheDocument();
    });

    it("removes read notifications from Unread and keeps the mark-all action", () => {
        const { rerender } = render(<NotificationBell />);
        fireEvent.click(screen.getByRole("button", { name: "Notifications" }));
        fireEvent.click(screen.getByRole("button", { name: "Mark all read" }));
        expect(fixture.markAll).toHaveBeenCalledOnce();
        fixture.notifications = fixture.notifications.map((n) => ({ ...n, read_at: new Date() }));
        fixture.unread = 0;
        rerender(<NotificationBell />);
        expect(screen.getByText("You're all caught up.")).toBeInTheDocument();
        expect(screen.queryByText("New reply")).not.toBeInTheDocument();
        expect(screen.queryByRole("button", { name: "Mark all read" })).not.toBeInTheDocument();
    });
});
