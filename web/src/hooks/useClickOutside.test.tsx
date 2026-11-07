// useClickOutside is how every dropdown closes: a press outside it, Escape (the
// innermost one only), or focus moving into or a tap landing in an iframe.

import React from "react";
import { createPortal } from "react-dom";
import { describe, it, expect, afterEach } from "vitest";
import { render, screen, fireEvent, cleanup, act } from "@testing-library/react";
import useClickOutside from "./useClickOutside";

afterEach(cleanup);

// A wrapper holding trigger and panel, plus an optional portaled floating
// layer it opens (a calendar).
function Drop({ name, layer = false, children }: { name: string; layer?: boolean; children?: React.ReactNode }) {
    const [open, setOpen] = React.useState(false);
    const ref = React.useRef<HTMLDivElement>(null);
    useClickOutside(open, () => setOpen(false), ref);
    return (
        <div ref={ref}>
            <button type="button" onClick={() => setOpen((o) => !o)}>
                {name}
            </button>
            {open && (
                <div data-testid={`${name}-panel`}>
                    {children}
                    {layer &&
                        createPortal(<div data-floating="" data-testid={`${name}-layer`} />, document.body)}
                </div>
            )}
        </div>
    );
}

const toggle = (name: string) => fireEvent.click(screen.getByRole("button", { name }));
const isOpen = (name: string) => !!screen.queryByTestId(`${name}-panel`);

describe("useClickOutside", () => {
    it("shields an outside iframe while open and restores its previous inline pointer events", () => {
        render(
            <>
                <Drop name="A" />
                <iframe title="body" style={{ pointerEvents: "auto" }} />
            </>,
        );
        const frame = screen.getByTitle("body");
        toggle("A");
        expect(frame.style.pointerEvents).toBe("none");
        toggle("A");
        expect(frame.style.pointerEvents).toBe("auto");
    });

    it("keeps an outside iframe shielded until the last nested layer closes", () => {
        render(
            <>
                <Drop name="Outer">
                    <Drop name="Inner" />
                </Drop>
                <iframe title="body" />
            </>,
        );
        const frame = screen.getByTitle("body");
        toggle("Outer");
        toggle("Inner");
        expect([isOpen("Outer"), isOpen("Inner")]).toEqual([true, true]);
        expect(frame.style.pointerEvents).toBe("none");
        toggle("Inner");
        expect(isOpen("Outer")).toBe(true);
        expect(frame.style.pointerEvents).toBe("none");
        toggle("Outer");
        expect(frame.style.pointerEvents).toBe("");
    });

    it("leaves a frame inside the layer's inline pointer events untouched", () => {
        render(
            <Drop name="A">
                <iframe title="preview" style={{ pointerEvents: "auto" }} />
            </Drop>,
        );
        toggle("A");
        expect(screen.getByTitle("preview").style.pointerEvents).toBe("auto");
    });

    it("closes on a parent-document press on the element holding an outside iframe", () => {
        render(
            <>
                <Drop name="A" />
                <div data-testid="body-wrapper">
                    <iframe title="body" />
                </div>
            </>,
        );
        toggle("A");
        fireEvent.pointerDown(screen.getByTestId("body-wrapper"));
        expect(isOpen("A")).toBe(false);
    });

    it("closes on a press outside, and not on one inside it or a floating layer it opened", () => {
        render(
            <>
                <Drop name="A" layer />
                <div data-testid="out" />
            </>,
        );
        toggle("A");
        fireEvent.pointerDown(screen.getByTestId("A-panel"));
        fireEvent.pointerDown(screen.getByTestId("A-layer"));
        expect(isOpen("A")).toBe(true);
        fireEvent.pointerDown(screen.getByTestId("out"));
        expect(isOpen("A")).toBe(false);
    });

    it("closes on a press elsewhere in the floating dialog card that holds it", () => {
        render(
            <div data-floating="" onMouseDown={(e) => e.stopPropagation()}>
                <Drop name="A" />
                <div data-testid="card-body" />
            </div>,
        );
        toggle("A");
        fireEvent.pointerDown(screen.getByTestId("card-body"));
        expect(isOpen("A")).toBe(false);
    });

    it("takes Escape for the innermost dropdown only, and keeps it from the dialog around it", () => {
        let dialogSawEscape = false;
        const onKey = (e: KeyboardEvent) => {
            if (e.key === "Escape") dialogSawEscape = true;
        };
        document.addEventListener("keydown", onKey);
        render(
            <Drop name="Outer">
                <Drop name="Inner" />
            </Drop>,
        );
        toggle("Outer");
        toggle("Inner");
        fireEvent.keyDown(document.body, { key: "Escape" });
        expect([isOpen("Outer"), isOpen("Inner")]).toEqual([true, false]);
        fireEvent.keyDown(document.body, { key: "Escape" });
        expect(isOpen("Outer")).toBe(false);
        expect(dialogSawEscape).toBe(false);
        fireEvent.keyDown(document.body, { key: "Escape" });
        expect(dialogSawEscape).toBe(true);
        document.removeEventListener("keydown", onKey);
    });

    it("leaves Escape to a confirm opened above it, and still takes it inside a modal", () => {
        const { unmount } = render(
            <>
                <Drop name="A" />
                <div role="alertdialog" />
            </>,
        );
        toggle("A");
        fireEvent.keyDown(document.body, { key: "Escape" });
        expect(isOpen("A")).toBe(true);
        unmount();

        render(
            <div aria-modal="true">
                <Drop name="B" />
            </div>,
        );
        toggle("B");
        fireEvent.keyDown(document.body, { key: "Escape" });
        expect(isOpen("B")).toBe(false);
    });

    it("closes the open dropdown when another one opens", () => {
        render(
            <>
                <Drop name="A" />
                <Drop name="B" />
            </>,
        );
        toggle("A");
        toggle("B");
        expect([isOpen("A"), isOpen("B")]).toEqual([false, true]);
    });

    it("closes a sibling floating root when another one opens, and keeps one opened inside a floating panel", () => {
        // A layer rooted on its own portaled [data-floating] panel, opened from
        // the keyboard like the textarea AI menus.
        function Floating({ name }: { name: string }) {
            const [open, setOpen] = React.useState(false);
            const ref = React.useRef<HTMLDivElement>(null);
            useClickOutside(open, () => setOpen(false), ref);
            return (
                <>
                    <button type="button" onClick={() => setOpen(true)}>
                        {name}
                    </button>
                    {createPortal(
                        <div ref={ref} data-floating="">
                            {open && <div data-testid={`${name}-panel`} />}
                        </div>,
                        document.body,
                    )}
                </>
            );
        }
        render(
            <>
                <Floating name="A" />
                <Floating name="B" />
                <div data-floating="">
                    <Drop name="C" />
                </div>
            </>,
        );
        toggle("A");
        toggle("B");
        expect([isOpen("A"), isOpen("B")]).toEqual([false, true]);
        toggle("C");
        expect([isOpen("B"), isOpen("C")]).toEqual([true, true]);
    });

    it("closes on a press inside an iframe, which only blurs the window", async () => {
        render(
            <>
                <Drop name="A" />
                <iframe title="body" />
            </>,
        );
        const settle = () =>
            act(async () => {
                await new Promise((r) => setTimeout(r, 30));
            });
        toggle("A");
        // Leaving the tab is not a press inside the page.
        fireEvent.blur(window);
        await settle();
        expect(isOpen("A")).toBe(true);
        screen.getByTitle("body").focus();
        fireEvent.blur(window);
        await settle();
        expect(isOpen("A")).toBe(false);
    });

    // A tap on a phone moves no focus, so only the frame's own document hears it.
    it("closes on a tap inside a same-origin frame that never takes focus", () => {
        render(
            <>
                <Drop name="A" />
                <iframe title="body" />
            </>,
        );
        const frameDoc = (screen.getByTitle("body") as HTMLIFrameElement).contentDocument!;
        toggle("A");
        fireEvent.pointerDown(frameDoc.body);
        expect(isOpen("A")).toBe(false);

        // And the listener goes with the layer: reopening still works once.
        toggle("A");
        expect(isOpen("A")).toBe(true);
        fireEvent.pointerDown(frameDoc.body);
        expect(isOpen("A")).toBe(false);
    });

    it("stays open on a press inside a frame it holds", () => {
        render(
            <Drop name="A">
                <iframe title="preview" />
            </Drop>,
        );
        toggle("A");
        const frameDoc = (screen.getByTitle("preview") as HTMLIFrameElement).contentDocument!;
        fireEvent.pointerDown(frameDoc.body);
        expect(isOpen("A")).toBe(true);
    });
});
