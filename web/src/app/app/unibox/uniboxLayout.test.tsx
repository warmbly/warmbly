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
import { describe, it, expect, vi, beforeAll, beforeEach, afterEach } from "vitest";
import { screen, act, fireEvent } from "@testing-library/react";
import { useAppStore } from "@/stores";
import { replyDraftKey } from "@/lib/unibox/replyDraft";
import { useOutboxStore } from "@/hooks/useOutboxStore";
import {
    UNIBOX_LIST_DEFAULT_WIDTH,
    UNIBOX_LIST_MAX_WIDTH,
    UNIBOX_LIST_MIN_WIDTH,
} from "@/stores/slices/uiSlice";
import {
    installLayoutShims,
    mount,
    resetScrollTops,
    resizeViewportTo,
    setViewportWidth,
    settle,
    SUITE,
} from "./uniboxHarness";

const threadFixture = vi.hoisted(() => ({ includeNewer: false }));

beforeAll(() => {
    installLayoutShims();
    // A 14" laptop: wide enough for the three-column inbox, which is the
    // viewport the issue is about.
    setViewportWidth(1512);
});

vi.mock("@/lib/api/client/Request", () => ({
    default: async (cfg: { url?: string }) => {
        const { route, ROWS } = await import("./uniboxHarness");
        const url = String(cfg?.url ?? "");
        if (threadFixture.includeNewer && url.startsWith("/unibox/thread?") && url.includes("thread-4")) {
            return { data: [ROWS[4], { ...ROWS[4], id: "newer-message" }], pagination: { has_more: false } };
        }
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

function separator(): HTMLElement {
    return screen.getByRole("separator", { name: /resize the conversation list/i });
}

function listColumn(): HTMLElement {
    const el = separator().previousElementSibling as HTMLElement | null;
    if (!el) throw new Error("conversation list column not found");
    return el;
}

// Pointer capture routes the move and the release back to the separator, so
// that is where the test drives them too.
async function drag(fromX: number, toX: number) {
    await act(async () => {
        fireEvent.pointerDown(separator(), { clientX: fromX, button: 0, pointerId: 1 });
        fireEvent.pointerMove(separator(), { clientX: toX, pointerId: 1 });
        fireEvent.pointerUp(separator(), { clientX: toX, pointerId: 1 });
    });
}

async function openThread(subject: string) {
    await act(async () => {
        fireEvent.click(screen.getByText(subject).closest('[role="button"]')!);
    });
    await settle();
}

describe("unibox desktop layout (#473)", SUITE, () => {
    beforeEach(() => {
        threadFixture.includeNewer = false;
        resetScrollTops();
        useAppStore.setState({
            navCollapsed: false,
            uniboxListWidth: UNIBOX_LIST_DEFAULT_WIDTH,
            uniboxContactRailOpen: true,
        });
    });

    afterEach(() => {
        vi.mocked(localStorage.getItem).mockReset();
    });

    it("reopens a personal draft on an older message when newer mail exists", async () => {
        threadFixture.includeNewer = true;
        useAppStore.setState({ currentOrganization: { id: "org-1", name: "Org", role: "owner" } });
        const key = replyDraftKey("u1", "org-1", "thread-4", "msg-4", "reply");
        vi.mocked(localStorage.getItem).mockImplementation((item: string) => item === key
            ? JSON.stringify({ to: ["s4@example.com"], cc: [], bcc: [], subject: "Re: Subject 4", body: "My older reply" })
            : null);
        await mount("/app/unibox/all");
        await settle();
        await openThread("Subject 4");
        expect(screen.getByPlaceholderText(/Write your reply/)).toHaveValue("My older reply");
        await act(async () => fireEvent.click(screen.getByLabelText("Close composer, keeping the draft")));
        await settle();
        expect(screen.queryByPlaceholderText(/Write your reply/)).toBeNull();
    });

    it("does not forward a different message when the forwarded one is gone", async () => {
        await mount("/app/unibox/all");
        await settle();
        act(() => useOutboxStore.getState().setReplyRestore({
            threadId: "thread-4", messageId: "no-longer-here", mode: "forward",
            to: ["x@example.com"], cc: [], bcc: [], subject: "Fwd: Subject 4", body: "FYI", emailAccountId: "mbox-1",
        }));
        await openThread("Subject 4");
        expect(screen.queryByPlaceholderText(/Add a note/)).toBeNull();
        // The thread stays answerable: the Reply/Forward bar is back.
        expect(screen.getByRole("button", { name: "Forward" })).toBeInTheDocument();
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

            expect(useAppStore.getState().navCollapsed).toBe(true);
            expect(aside.className).toContain("md:w-14");
            // Same destination, no VISIBLE label: the name moves into a
            // visually hidden span, because lucide marks its svg aria-hidden
            // and the link would otherwise announce as nothing at all. It must
            // not become an aria-label: that would override the whole subtree
            // and silence the unread count nested in the same link.
            expect(settingsLink().getAttribute("aria-label")).toBeNull();
            expect(settingsLink().textContent).toBe("Settings");
            expect(settingsLink().querySelector("span")?.className).toContain("sr-only");

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

            expect(useAppStore.getState().navCollapsed).toBe(false);
        });
    });

    describe("resizable conversation list", () => {
        it("drags to a new width, clamped at the bounds", async () => {
            await mount("/app/unibox/all");
            await settle();

            expect(listColumn().style.getPropertyValue("--unibox-list-w")).toBe("360px");

            // Pressing inside the 6px handle and not moving must not resize:
            // the drag is relative to where it was grabbed, so the divider
            // stays under the cursor instead of jumping out to meet it.
            await drag(366, 366);
            expect(useAppStore.getState().uniboxListWidth).toBe(
                UNIBOX_LIST_DEFAULT_WIDTH,
            );

            // And a real drag moves by the distance travelled.
            await drag(360, 480);
            expect(useAppStore.getState().uniboxListWidth).toBe(480);
            expect(listColumn().style.getPropertyValue("--unibox-list-w")).toBe("480px");

            // Past the far bound the handle parks rather than swallowing the
            // thread pane.
            await drag(480, 4000);
            expect(useAppStore.getState().uniboxListWidth).toBe(UNIBOX_LIST_MAX_WIDTH);

            // ...and past the near bound it parks at the minimum.
            await drag(UNIBOX_LIST_MAX_WIDTH, 0);
            expect(useAppStore.getState().uniboxListWidth).toBe(UNIBOX_LIST_MIN_WIDTH);
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

        it("ignores a non-primary button, so a right-click cannot strand a drag", async () => {
            await mount("/app/unibox/all");
            await settle();

            await act(async () => {
                fireEvent.pointerDown(separator(), { clientX: 360, button: 2, pointerId: 1 });
                fireEvent.pointerMove(separator(), { clientX: 600, pointerId: 1 });
            });

            expect(useAppStore.getState().uniboxListWidth).toBe(UNIBOX_LIST_DEFAULT_WIDTH);
            // A context menu eats the pointerup, so a drag that started here
            // would leave the whole app unselectable.
            expect(document.body.style.userSelect).toBe("");
        });

        it("keeps the chosen width when a thread is opened", async () => {
            await mount("/app/unibox/all");
            await settle();

            await drag(360, 500);

            await openThread("Subject 4");
            expect(listColumn().style.getPropertyValue("--unibox-list-w")).toBe("500px");
        });
    });

    describe("contact rail", () => {
        it("starts closed by default, and each state sticks thread after thread", async () => {
            const initial = useAppStore.getInitialState().uniboxContactRailOpen;
            expect(initial).toBe(false);
            useAppStore.setState({ uniboxContactRailOpen: initial });
            await mount("/app/unibox/all");
            await settle();
            await openThread("Subject 4");
            expect(screen.getByLabelText("Show contact panel")).toBeTruthy();

            await act(async () => {
                fireEvent.click(screen.getByLabelText("Show contact panel"));
            });
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

        it("does not bring the narrow overlay back after a trip up past lg", async () => {
            await mount("/app/unibox/all");
            await settle();
            await openThread("Subject 4");

            // Narrow: the panel is an overlay on top of the thread, and starts
            // closed there whatever the wide-screen preference says.
            await resizeViewportTo(900);
            expect(screen.getByLabelText("Show contact panel")).toBeTruthy();

            // Open the overlay, then widen and narrow again. The overlay state
            // has to be dropped on the way up, or the drawer and its backdrop
            // land back over the thread with nobody asking for them.
            await act(async () => {
                fireEvent.click(screen.getByLabelText("Show contact panel"));
            });
            expect(screen.getAllByLabelText("Hide contact panel").length).toBeGreaterThan(0);

            await resizeViewportTo(1512);
            await resizeViewportTo(900);
            expect(screen.getByLabelText("Show contact panel")).toBeTruthy();
        });
    });
});
