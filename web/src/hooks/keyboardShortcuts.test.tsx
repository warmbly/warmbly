// Issue #484: the `?` modal advertised nine shortcuts with no implementation,
// and a tenth (`g k`) that a single-key branch returned in front of.
//
// The fix is structural — the modal renders the same registry the dispatcher
// walks — so the first test here is the invariant, and the rest are the ten
// individual keys, because a registry only helps if the dispatch rules around
// it (pending sequences, typing, availability) stay right.

import React from "react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, act, fireEvent, cleanup } from "@testing-library/react";
import { MemoryRouter, Routes, Route, useLocation } from "react-router-dom";
import {
    dispatchPanelShortcut,
    globalShortcuts,
    panelShortcuts,
    useKeyboardShortcuts,
    visibleShortcuts,
    type PanelCtx,
} from "./useKeyboardShortcuts";
import { useShortcutActions, type ShortcutActions } from "./useShortcutActions";
import { useAppStore } from "@/stores";
import { useComposeStore } from "@/hooks/useComposeStore";

function Location() {
    return <span data-testid="path">{useLocation().pathname}</span>;
}

function Shortcuts() {
    useKeyboardShortcuts();
    return null;
}

function Provider({ actions }: { actions: ShortcutActions }) {
    useShortcutActions(actions);
    return null;
}

function mount(ui?: React.ReactNode) {
    return render(
        <MemoryRouter initialEntries={["/app/start"]}>
            <Shortcuts />
            {ui}
            <Routes>
                <Route path="*" element={<Location />} />
            </Routes>
        </MemoryRouter>,
    );
}

const path = () => screen.getByTestId("path").textContent;

function press(key: string, init: Partial<KeyboardEventInit> = {}, target?: Element) {
    act(() => {
        fireEvent.keyDown(target ?? document.body, { key, ...init });
    });
}

beforeEach(() => {
    useAppStore.getState().clearSequence();
    useAppStore.setState({ navCollapsed: false, commandPaletteOpen: false });
    useComposeStore.setState({ open: false });
});
afterEach(cleanup);

describe("the shortcut registry (#484)", () => {
    it("has one dispatchable entry behind every row the modal can show", () => {
        const all = [...globalShortcuts, ...panelShortcuts];
        for (const s of all) {
            expect(typeof s.run, `${s.keys.join("+")} has no run`).toBe("function");
            expect(s.description.length).toBeGreaterThan(0);
        }
        for (const s of globalShortcuts) {
            // Exactly one dispatch form: a row with neither is unreachable, and
            // one with both would fire twice.
            expect(
                [s.match, s.sequence].filter(Boolean).length,
                `${s.keys.join("+")} needs exactly one of match/sequence`,
            ).toBe(1);
        }
        // What the modal renders is these same objects, not a parallel list.
        for (const group of ["navigation", "list", "actions", "assistant"] as const) {
            for (const row of visibleShortcuts(group)) {
                expect(all).toContain(row);
            }
        }
    });

    it("shows no key twice", () => {
        const seen = [...globalShortcuts, ...panelShortcuts].map((s) =>
            `${s.group}:${s.keys.join("+")}`,
        );
        expect(new Set(seen).size).toBe(seen.length);
    });
});

describe("navigation sequences", () => {
    it("routes `g k` to API keys instead of losing it to the `k` branch", () => {
        // The regression: `k` had a single-press handler that returned before
        // the sequence could ever resolve, so this one route of eleven was dead.
        mount(<Provider actions={{ listMove: vi.fn() }} />);

        press("g");
        press("k");
        expect(path()).toBe("/app/api-keys");
    });

    it("still routes the other letters", () => {
        mount();
        press("g");
        press("u");
        expect(path()).toBe("/app/unibox");
    });

    it("leaves a modifier combo alone mid-sequence", () => {
        mount();
        press("g");
        // Half a second of `g` must not turn the command palette into a route.
        press("k", { ctrlKey: true });
        expect(path()).toBe("/app/start");
        expect(useAppStore.getState().commandPaletteOpen).toBe(true);
        // ...and the abandoned sequence goes with it, so the next letter is
        // not read as the second half of a `g`.
        expect(useAppStore.getState().keySequence).toEqual([]);
    });

    it("swallows a letter that completes no sequence", () => {
        mount();
        press("g");
        // `n` composes on its own; mid-sequence it must not, or every mistyped
        // route pops a compose window.
        press("n");
        expect(useComposeStore.getState().open).toBe(false);
        expect(path()).toBe("/app/start");
        expect(useAppStore.getState().keySequence).toEqual([]);
    });
});

describe("list shortcuts", () => {
    it("do nothing, and are not shown, when no list is on screen", () => {
        mount();
        expect(visibleShortcuts("list")).toEqual([]);

        // The old failure mode was the opposite: shown, pressed, nothing.
        press("j");
        press("k");
        press("Enter");
        expect(path()).toBe("/app/start");
    });

    it("reach the list that registered them", () => {
        const listMove = vi.fn();
        const listEdge = vi.fn();
        const listOpen = vi.fn();
        const listDeselect = vi.fn();
        mount(<Provider actions={{ listMove, listEdge, listOpen, listDeselect }} />);

        expect(visibleShortcuts("list").map((r) => r.keys.join(""))).toEqual([
            "j",
            "k",
            "gg",
            "G",
            "Enter",
            "Escape",
        ]);

        press("j");
        expect(listMove).toHaveBeenCalledWith(1);
        press("k");
        expect(listMove).toHaveBeenCalledWith(-1);
        press("G", { shiftKey: true });
        expect(listEdge).toHaveBeenCalledWith("last");
        press("g");
        press("g");
        expect(listEdge).toHaveBeenCalledWith("first");
        press("Enter");
        expect(listOpen).toHaveBeenCalled();
        press("Escape");
        expect(listDeselect).toHaveBeenCalled();
    });

    it("leaves Enter to whatever the user has focused", () => {
        const listOpen = vi.fn();
        mount(
            <>
                <Provider actions={{ listOpen }} />
                <button type="button">Send</button>
            </>,
        );

        press("Enter", {}, screen.getByRole("button", { name: "Send" }));
        expect(listOpen).not.toHaveBeenCalled();
    });

    it("leaves Escape to the innermost layer", () => {
        const listDeselect = vi.fn();
        mount(
            <>
                <Provider actions={{ listDeselect }} />
                <div role="dialog" aria-label="Confirm" />
            </>,
        );

        press("Escape");
        expect(listDeselect).not.toHaveBeenCalled();
    });

    it("suspends while the screen says something else owns the keyboard", () => {
        const listMove = vi.fn();
        function Suspended() {
            useShortcutActions({ listMove }, { suspended: true });
            return null;
        }
        mount(<Suspended />);

        press("j");
        expect(listMove).not.toHaveBeenCalled();
        expect(visibleShortcuts("list")).toEqual([]);
    });
});

describe("focus search", () => {
    it("finds the shared search primitive's input", () => {
        mount(<input data-search-input="" data-testid="search" />);

        const row = visibleShortcuts("actions").find((r) => r.keys[0] === "/");
        expect(row).toBeTruthy();

        press("/");
        expect(document.activeElement).toBe(screen.getByTestId("search"));
    });

    it("prefers the screen's own search box over the first one in the DOM", () => {
        const focusSearch = vi.fn();
        mount(
            <>
                <input data-search-input="" data-testid="search" />
                <Provider actions={{ focusSearch }} />
            </>,
        );

        press("/");
        expect(focusSearch).toHaveBeenCalled();
        expect(document.activeElement).not.toBe(screen.getByTestId("search"));
    });

    it("is hidden on a screen with nothing to search", () => {
        // The original bug in one line: the handler queried an attribute that
        // no element in the app carried.
        mount();
        expect(visibleShortcuts("actions").some((r) => r.keys[0] === "/")).toBe(false);
    });
});

describe("typing", () => {
    it("ignores bare keys in an input but keeps the modifier combos", () => {
        mount(<input data-testid="field" />);
        const field = screen.getByTestId("field");

        press("b", {}, field);
        expect(useAppStore.getState().navCollapsed).toBe(false);

        press("k", { ctrlKey: true }, field);
        expect(useAppStore.getState().commandPaletteOpen).toBe(true);
    });

    it("collapses the nav on a bare `b`", () => {
        mount();
        press("b");
        expect(useAppStore.getState().navCollapsed).toBe(true);
    });
});

describe("the assistant panel's own keys", () => {
    function ctx(): PanelCtx & Record<string, ReturnType<typeof vi.fn>> {
        return {
            close: vi.fn(),
            cycleTab: vi.fn(),
            newTab: vi.fn(),
            closeTab: vi.fn(),
            minimize: vi.fn(),
            togglePopOut: vi.fn(),
            canPopOut: true,
        } as never;
    }
    const evt = (init: Partial<React.KeyboardEvent>) =>
        ({
            preventDefault: vi.fn(),
            stopPropagation: vi.fn(),
            key: "",
            code: "",
            altKey: false,
            ctrlKey: false,
            metaKey: false,
            ...init,
        }) as React.KeyboardEvent;

    it("dispatches from the same registry the modal shows", () => {
        const c = ctx();
        expect(dispatchPanelShortcut(evt({ key: "Escape" }), c)).toBe(true);
        expect(c.close).toHaveBeenCalled();

        expect(dispatchPanelShortcut(evt({ key: "]", ctrlKey: true }), c)).toBe(true);
        expect(c.cycleTab).toHaveBeenCalledWith(1);

        // Alt combos match on code: macOS Option remaps e.key to a symbol.
        expect(dispatchPanelShortcut(evt({ code: "KeyN", altKey: true, key: "ˆ" }), c)).toBe(
            true,
        );
        expect(c.newTab).toHaveBeenCalled();

        expect(dispatchPanelShortcut(evt({ key: "x" }), c)).toBe(false);
    });

    it("does not cancel Escape, which the panel is not the only owner of", () => {
        const c = ctx();
        const e = evt({ key: "Escape" });
        dispatchPanelShortcut(e, c);
        expect(e.preventDefault).not.toHaveBeenCalled();
        expect(e.stopPropagation).toHaveBeenCalled();
    });
});

describe("the ? modal", () => {
    it("renders the registry, so it can only show what runs", async () => {
        const { ShortcutsModal } = await import("@/components/shared/ShortcutsModal");
        useAppStore.setState({ shortcutsModalOpen: true });

        const { rerender } = render(
            <>
                <ShortcutsModal />
            </>,
        );

        // Navigation is unconditional; the list keys are not, because nothing
        // on this screen answers them.
        expect(screen.getByText("Go to API Keys")).toBeTruthy();
        expect(screen.queryByText("Move down in list")).toBeNull();
        expect(screen.queryByText("Focus search")).toBeNull();

        rerender(
            <>
                <Provider actions={{ listMove: vi.fn() }} />
                <input data-search-input="" />
                <ShortcutsModal />
            </>,
        );
        act(() => {
            useAppStore.setState({ shortcutsModalOpen: false });
        });
        act(() => {
            useAppStore.setState({ shortcutsModalOpen: true });
        });

        expect(screen.getByText("Move down in list")).toBeTruthy();
        expect(screen.getByText("Focus search")).toBeTruthy();
    });
});
