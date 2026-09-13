// Issue #473: desktop layout ergonomics.
//
// Three separate things a laptop user asked for, pinned here because each one
// is easy to undo by accident:
//
//   - the `b` shortcut and the rail's own button collapse the left navigation.
//     `b` was documented in the shortcuts modal and wired to the store for a
//     long time while nothing read the flag, so it did nothing at all.
//   - the conversation list is drag-resizable against the thread pane.
//   - closing the CRM rail sticks. ThreadView is keyed on the thread id, so
//     component state puts the rail back over the next conversation; the
//     default has already flipped twice (#402, then 568bdb48).
//
// All of it renders through the real shell, because what is being pinned is
// which state survives a remount.

import React from "react";
import { describe, it, expect, vi, beforeAll, beforeEach } from "vitest";
import { screen, act, fireEvent } from "@testing-library/react";
import { useAppStore } from "@/stores";
import {
    UNIBOX_LIST_DEFAULT_WIDTH,
    UNIBOX_LIST_MAX_WIDTH,
    UNIBOX_LIST_MIN_WIDTH,
} from "@/stores/slices/uiSlice";
import {
    installLayoutShims,
    mount,
    resetScrollTops,
    setViewportWidth,
    settle,
} from "./uniboxHarness";

beforeAll(() => {
    installLayoutShims();
    // A 14" laptop: wide enough for the three-column inbox, which is the
    // viewport the issue is about.
    setViewportWidth(1512);
});

vi.mock("@/lib/api/client/Request", () => ({
    default: async (cfg: { url?: string }) => {
        const { route } = await import("./uniboxHarness");
        return route(String(cfg?.url ?? ""));
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
        useSocket: () => ({
            isConnected: false,
            subscribeToChannel: () => () => {},
            pushToChannel: () => {},
            socket: null,
            status: "closed",
        }),
        useChannel: () => ({ state: "closed", push: () => {}, channel: null }),
        useChannelEvent: () => {},
        useChannelSubscription: () => {},
    };
});

// Mounting the whole shell in jsdom is slow, and slower again when the two
// unibox suites run alongside each other, so these get more than the 5s
// default rather than flaking on a loaded machine.
const SUITE = { timeout: 30_000 };

function separator(): HTMLElement {
    return screen.getByRole("separator", { name: /resize the conversation list/i });
}

function listColumn(): HTMLElement {
    const el = separator().previousElementSibling as HTMLElement | null;
    if (!el) throw new Error("conversation list column not found");
    return el;
}

async function openThread(subject: string) {
    await act(async () => {
        fireEvent.click(screen.getByText(subject).closest("button")!);
    });
    await settle();
}

describe("unibox desktop layout (#473)", SUITE, () => {
    beforeEach(() => {
        resetScrollTops();
        useAppStore.setState({
            sidebarCollapsed: false,
            uniboxListWidth: UNIBOX_LIST_DEFAULT_WIDTH,
            uniboxContactRailOpen: true,
        });
    });

    describe("collapsible left navigation", () => {
        it("collapses to an icon rail and back, from the button and from `b`", async () => {
            await mount("/app/unibox/all");
            await settle();

            // Expanded: the nav rows carry their labels. Queried by selector
            // rather than by role — computing roles across the whole shell is
            // slow enough in jsdom to dominate the test.
            const settingsLink = () =>
                document.querySelector<HTMLAnchorElement>('aside a[href="/app/settings"]')!;
            expect(settingsLink().textContent).toContain("Settings");

            const collapse = screen.getByLabelText("Collapse sidebar");
            const aside = collapse.closest("aside")!;
            expect(aside.className).toContain("md:w-64");

            await act(async () => {
                fireEvent.click(collapse);
            });

            expect(useAppStore.getState().sidebarCollapsed).toBe(true);
            expect(aside.className).toContain("md:w-14");
            // Same destination, no visible label: an icon-only row. The name
            // moves to aria-label, because lucide marks its svg aria-hidden and
            // the link would otherwise announce as nothing at all.
            expect(settingsLink().textContent).toBe("");
            expect(settingsLink().getAttribute("aria-label")).toBe("Settings");

            // `b` is the documented shortcut for the same thing. It was wired to
            // the store while nothing rendered from it; this is what makes it
            // real, so assert on the rail and not on the store flag.
            await act(async () => {
                fireEvent.keyDown(document.body, { key: "b" });
            });
            expect(aside.className).toContain("md:w-64");
            expect(screen.getByLabelText("Collapse sidebar")).toBeTruthy();
            expect(settingsLink().textContent).toContain("Settings");
        });

        it("does not eat `b` while the user is typing", async () => {
            await mount("/app/unibox/all");
            await settle();

            const search = screen.getByPlaceholderText(/^Search/i);
            await act(async () => {
                fireEvent.keyDown(search, { key: "b" });
            });

            expect(useAppStore.getState().sidebarCollapsed).toBe(false);
        });
    });

    describe("resizable conversation list", () => {
        it("drags to a new width, clamped at the bounds", async () => {
            await mount("/app/unibox/all");
            await settle();

            expect(listColumn().style.getPropertyValue("--unibox-list-w")).toBe("360px");

            // jsdom reports every rect as zero, so clientX IS the width here.
            await act(async () => {
                fireEvent.pointerDown(separator(), { clientX: 360 });
                window.dispatchEvent(
                    new MouseEvent("pointermove", { clientX: 480, bubbles: true }),
                );
                window.dispatchEvent(new MouseEvent("pointerup", { bubbles: true }));
            });

            expect(useAppStore.getState().uniboxListWidth).toBe(480);
            expect(listColumn().style.getPropertyValue("--unibox-list-w")).toBe("480px");

            // Past the far bound the handle parks rather than swallowing the
            // thread pane.
            await act(async () => {
                fireEvent.pointerDown(separator(), { clientX: 480 });
                window.dispatchEvent(
                    new MouseEvent("pointermove", { clientX: 4000, bubbles: true }),
                );
                window.dispatchEvent(new MouseEvent("pointerup", { bubbles: true }));
            });
            expect(useAppStore.getState().uniboxListWidth).toBe(UNIBOX_LIST_MAX_WIDTH);
        });

        it("takes the ARIA window-splitter keys", async () => {
            await mount("/app/unibox/all");
            await settle();

            const press = async (key: string, shiftKey = false) => {
                await act(async () => {
                    fireEvent.keyDown(separator(), { key, shiftKey });
                });
                return useAppStore.getState().uniboxListWidth;
            };

            expect(await press("ArrowRight")).toBe(376);
            expect(await press("ArrowLeft", true)).toBe(328);
            expect(await press("End")).toBe(UNIBOX_LIST_MAX_WIDTH);
            expect(await press("Home")).toBe(UNIBOX_LIST_MIN_WIDTH);
            expect(await press("Enter")).toBe(UNIBOX_LIST_DEFAULT_WIDTH);
        });

        it("keeps the chosen width when a thread is opened", async () => {
            await mount("/app/unibox/all");
            await settle();

            await act(async () => {
                fireEvent.pointerDown(separator(), { clientX: 360 });
                window.dispatchEvent(
                    new MouseEvent("pointermove", { clientX: 500, bubbles: true }),
                );
                window.dispatchEvent(new MouseEvent("pointerup", { bubbles: true }));
            });

            await openThread("Subject 4");
            expect(listColumn().style.getPropertyValue("--unibox-list-w")).toBe("500px");
        });
    });

    describe("contact rail", () => {
        it("opens by default and stays closed once closed, thread after thread", async () => {
            await mount("/app/unibox/all");
            await settle();

            await openThread("Subject 4");
            // Default is still open, which is what 568bdb48 settled on.
            const toggle = screen.getByLabelText("Hide contact panel");

            await act(async () => {
                fireEvent.click(toggle);
            });
            expect(screen.getByLabelText("Show contact panel")).toBeTruthy();

            // The reader is keyed on the thread id, so this remounts it. The
            // rail must not come back.
            await openThread("Subject 5");
            expect(screen.getByLabelText("Show contact panel")).toBeTruthy();
            expect(useAppStore.getState().uniboxContactRailOpen).toBe(false);

            // Re-opening sticks the same way.
            await act(async () => {
                fireEvent.click(screen.getByLabelText("Show contact panel"));
            });
            await openThread("Subject 6");
            expect(screen.getByLabelText("Hide contact panel")).toBeTruthy();
        });
    });
});
