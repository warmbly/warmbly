// PopoverMenu closes itself: a press anywhere outside it, Escape, or another
// menu opening. A menu opened from inside another one is its child and leaves
// the parent open, and Escape takes the innermost first.

import React from "react";
import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, fireEvent, cleanup } from "@testing-library/react";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuTrigger,
} from "./popover-menu";

// Exit animations never finish in jsdom; a closed menu unmounts at once instead.
vi.mock("framer-motion", async (importOriginal) => ({
    ...(await importOriginal<Record<string, unknown>>()),
    AnimatePresence: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
});

function Menu({ name, children }: { name: string; children?: React.ReactNode }) {
    return (
        <PopoverMenu>
            <PopoverMenuTrigger>{name}</PopoverMenuTrigger>
            <PopoverMenuContent>
                <PopoverMenuItem>{`${name} item`}</PopoverMenuItem>
                {children}
            </PopoverMenuContent>
        </PopoverMenu>
    );
}

const open = (name: string) => fireEvent.click(screen.getByRole("button", { name }));
const shown = () => screen.queryAllByRole("menuitem").map((i) => i.textContent);

describe("PopoverMenu", () => {
    it("clamps a right-aligned feed to a narrow screen", () => {
        vi.stubGlobal("innerWidth", 320);
        vi.spyOn(HTMLElement.prototype, "offsetWidth", "get").mockReturnValue(304);
        vi.spyOn(HTMLElement.prototype, "offsetHeight", "get").mockReturnValue(400);
        vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(280, 8, 28, 28));
        render(
            <PopoverMenu align="end">
                <PopoverMenuTrigger>Notifications</PopoverMenuTrigger>
                <PopoverMenuContent minWidth={340} role="dialog" aria-label="Notifications">
                    Feed
                </PopoverMenuContent>
            </PopoverMenu>,
        );
        open("Notifications");
        const feed = screen.getByRole("dialog", { name: "Notifications" });
        expect(feed).toHaveStyle({ minWidth: "304px", left: "8px", visibility: "visible" });
        expect(parseFloat(feed.style.top) + 400).toBeLessThan(window.innerHeight - 8);
    });

    it("keeps an upward profile menu outside a transformed, clipped sidebar", () => {
        vi.spyOn(HTMLElement.prototype, "offsetWidth", "get").mockReturnValue(232);
        vi.spyOn(HTMLElement.prototype, "offsetHeight", "get").mockReturnValue(160);
        vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(8, window.innerHeight - 40, 232, 32));
        const { container } = render(
            <aside style={{ overflow: "hidden", transform: "translateX(0)" }}>
                <PopoverMenu side="top">
                    <PopoverMenuTrigger>My profile</PopoverMenuTrigger>
                    <PopoverMenuContent minWidth={232}>
                        <PopoverMenuItem>Settings</PopoverMenuItem>
                    </PopoverMenuContent>
                </PopoverMenu>
            </aside>,
        );
        open("My profile");
        const menu = screen.getByRole("menu");
        expect(container.contains(menu)).toBe(false);
        expect(menu.parentElement).toBe(document.body);
        expect(menu).toHaveStyle({ position: "fixed", visibility: "visible" });
        expect(parseFloat(menu.style.top)).toBeGreaterThanOrEqual(8);
        expect(parseFloat(menu.style.top) + 160).toBeLessThan(window.innerHeight - 8);
    });

    it("closes on a press outside, even inside a card that stops mousedown", () => {
        render(
            <>
                <Menu name="A" />
                <div data-testid="card" onMouseDown={(e) => e.stopPropagation()} />
            </>,
        );
        open("A");
        fireEvent.pointerDown(screen.getByRole("menuitem", { name: "A item" }));
        expect(shown()).toEqual(["A item"]);
        fireEvent.pointerDown(screen.getByTestId("card"));
        expect(shown()).toEqual([]);
    });

    it("toggles from its own trigger instead of closing and reopening", () => {
        render(<Menu name="A" />);
        open("A");
        fireEvent.pointerDown(screen.getByRole("button", { name: "A" }));
        open("A");
        expect(shown()).toEqual([]);
    });

    it("closes the open menu when another one opens", () => {
        render(
            <>
                <Menu name="A" />
                <Menu name="B" />
            </>,
        );
        open("A");
        open("B");
        expect(shown()).toEqual(["B item"]);
    });

    it("keeps a parent open under a menu opened from inside it, and Escape closes the child first", () => {
        render(
            <Menu name="Parent">
                <Menu name="Child" />
            </Menu>,
        );
        open("Parent");
        open("Child");
        expect(shown()).toEqual(["Parent item", "Child item"]);
        fireEvent.pointerDown(screen.getByRole("menuitem", { name: "Child item" }));
        expect(shown()).toEqual(["Parent item", "Child item"]);

        fireEvent.keyDown(document.body, { key: "Escape" });
        expect(shown()).toEqual(["Parent item"]);
        fireEvent.keyDown(document.body, { key: "Escape" });
        expect(shown()).toEqual([]);
    });

    it("keeps every ancestor open three levels deep, and a sibling replaces only the branch", () => {
        render(
            <Menu name="Top">
                <Menu name="Mid">
                    <Menu name="Leaf" />
                </Menu>
                <Menu name="Side" />
            </Menu>,
        );
        open("Top");
        open("Mid");
        open("Leaf");
        expect(shown()).toEqual(["Top item", "Mid item", "Leaf item"]);
        open("Side");
        expect(shown()).toEqual(["Top item", "Side item"]);
    });

    it("stays on one stack entry when a controlled parent re-renders it", () => {
        function Controlled() {
            const [o, setO] = React.useState(false);
            const [, tick] = React.useState(0);
            return (
                <>
                    <PopoverMenu open={o} onOpenChange={(v) => setO(v)}>
                        <PopoverMenuTrigger>C</PopoverMenuTrigger>
                        <PopoverMenuContent>
                            <PopoverMenuItem closeOnSelect={false} onSelect={() => tick((n) => n + 1)}>
                                C item
                            </PopoverMenuItem>
                            <Menu name="Inner" />
                        </PopoverMenuContent>
                    </PopoverMenu>
                </>
            );
        }
        render(<Controlled />);
        open("C");
        open("Inner");
        // A re-render of the parent must not push it back above the child.
        fireEvent.click(screen.getByRole("menuitem", { name: "C item" }));
        fireEvent.keyDown(document.body, { key: "Escape" });
        expect(shown()).toEqual(["C item"]);
    });
});
