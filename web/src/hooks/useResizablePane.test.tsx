// The shared pane-resize gesture (#485). The unibox splitter is covered end to
// end through the real shell in uniboxLayout.test.tsx; this pins the parts that
// are shared with the assistant panel and easy to regress in either call site:
// what reaches the store, what reaches the DOM, and which gestures do nothing.

import React from "react";
import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, act, fireEvent, cleanup } from "@testing-library/react";
import { capturePointerDrag, useResizablePane } from "./useResizablePane";

afterEach(cleanup);

const MIN = 200;
const MAX = 600;
const DEFAULT = 400;

function Pane({
    onChange,
    direction = 1,
    initial = DEFAULT,
    max = MAX,
}: {
    onChange?: (w: number) => void;
    direction?: 1 | -1;
    initial?: number;
    max?: number;
}) {
    const [value, setValue] = React.useState(initial);
    const paneRef = React.useRef<HTMLDivElement>(null);
    const { width, separatorProps } = useResizablePane({
        value,
        onChange: (w) => {
            onChange?.(w);
            setValue(w);
        },
        min: MIN,
        max,
        defaultValue: DEFAULT,
        paneRef,
        cssVar: "--w",
        direction,
        label: "Resize the pane",
        controls: "pane",
        valueText: (w) => `Pane ${w} pixels`,
    });
    return (
        <>
            <div
                id="pane"
                data-testid="pane"
                ref={paneRef}
                style={{ "--w": `${width}px` } as React.CSSProperties}
            />
            <div {...separatorProps} data-testid="sep" />
        </>
    );
}

const sep = () => screen.getByTestId("sep");
const paneVar = () =>
    screen.getByTestId("pane").style.getPropertyValue("--w");

function down(x: number, button = 0) {
    act(() => {
        fireEvent.pointerDown(sep(), { clientX: x, button, pointerId: 1 });
    });
}
function move(x: number) {
    act(() => {
        fireEvent.pointerMove(sep(), { clientX: x, pointerId: 1 });
    });
}
function up(x: number) {
    act(() => {
        fireEvent.pointerUp(sep(), { clientX: x, pointerId: 1 });
    });
}
function key(k: string, init: Partial<KeyboardEventInit> = {}) {
    act(() => {
        fireEvent.keyDown(sep(), { key: k, ...init });
    });
}

describe("useResizablePane", () => {
    it("writes the DOM every frame and the store once, on release", () => {
        const onChange = vi.fn();
        render(<Pane onChange={onChange} />);

        down(400);
        move(450);
        move(500);

        // The store is persisted: a write per frame is a JSON.stringify and a
        // localStorage.setItem per frame.
        expect(onChange).not.toHaveBeenCalled();
        expect(paneVar()).toBe("500px");

        up(500);
        expect(onChange).toHaveBeenCalledTimes(1);
        expect(onChange).toHaveBeenCalledWith(500);
    });

    it("commits nothing when the gesture never moved", () => {
        const onChange = vi.fn();
        render(<Pane onChange={onChange} />);

        // A click on the handle of a pane the viewport is already capping would
        // otherwise overwrite the width chosen on a bigger monitor.
        down(400);
        up(400);
        expect(onChange).not.toHaveBeenCalled();
    });

    it("ignores a non-primary button", () => {
        const onChange = vi.fn();
        render(<Pane onChange={onChange} />);

        // Chromium runs a nested loop for the context menu and eats the
        // pointerup, so a right-drag that started would never end.
        down(400, 2);
        move(500);
        up(500);
        expect(onChange).not.toHaveBeenCalled();
        expect(paneVar()).toBe("400px");
    });

    it("stops at both bounds", () => {
        const onChange = vi.fn();
        render(<Pane onChange={onChange} />);

        down(400);
        move(9000);
        up(9000);
        expect(onChange).toHaveBeenLastCalledWith(MAX);

        down(MAX);
        move(-9000);
        up(-9000);
        expect(onChange).toHaveBeenLastCalledWith(MIN);
    });

    it("widens toward its own edge when the handle is on the right of the pane", () => {
        const onChange = vi.fn();
        // The assistant panel docked to the right edge: its handle is on its
        // inner (left) edge, so dragging left makes it bigger.
        render(<Pane onChange={onChange} direction={-1} />);

        down(400);
        move(300);
        up(300);
        expect(onChange).toHaveBeenCalledWith(500);
    });

    it("releases the body lock on cancel, and a cancelled drag stops moving", () => {
        const onChange = vi.fn();
        render(<Pane onChange={onChange} />);

        down(400);
        move(450);
        expect(document.body.style.userSelect).toBe("none");

        act(() => {
            fireEvent.pointerCancel(sep(), { pointerId: 1 });
        });
        expect(document.body.style.userSelect).toBe("");
        expect(document.body.style.cursor).toBe("");
        expect(onChange).toHaveBeenCalledTimes(1);

        // Without the teardown the pane would keep following the cursor with no
        // button held.
        onChange.mockClear();
        move(600);
        expect(onChange).not.toHaveBeenCalled();
        expect(paneVar()).toBe("450px");
    });

    it("takes the window-splitter keys, with the arrows following the edge", () => {
        const onChange = vi.fn();
        const { unmount } = render(<Pane onChange={onChange} />);

        key("ArrowRight");
        expect(onChange).toHaveBeenLastCalledWith(DEFAULT + 16);
        key("ArrowLeft", { shiftKey: true });
        expect(onChange).toHaveBeenLastCalledWith(DEFAULT + 16 - 48);
        key("Home");
        expect(onChange).toHaveBeenLastCalledWith(MIN);
        key("End");
        expect(onChange).toHaveBeenLastCalledWith(MAX);
        key("Enter");
        expect(onChange).toHaveBeenLastCalledWith(DEFAULT);

        const calls = onChange.mock.calls.length;
        key("q");
        expect(onChange.mock.calls.length).toBe(calls);

        unmount();
        onChange.mockClear();
        render(<Pane onChange={onChange} direction={-1} />);
        // Same key, other edge: the separator still moves left, which on this
        // side makes the pane wider.
        key("ArrowLeft");
        expect(onChange).toHaveBeenLastCalledWith(DEFAULT + 16);
    });

    it("announces the width it is actually rendering, not the stored one", () => {
        // A preference from a bigger monitor, on a window that cannot give it.
        render(<Pane initial={580} max={320} />);

        const s = sep();
        expect(s.getAttribute("aria-valuenow")).toBe("320");
        expect(s.getAttribute("aria-valuemax")).toBe("320");
        expect(s.getAttribute("aria-valuemin")).toBe(String(MIN));
        expect(s.getAttribute("aria-valuetext")).toBe("Pane 320 pixels");
        expect(s.getAttribute("aria-controls")).toBe("pane");
        expect(s.getAttribute("role")).toBe("separator");
        expect(s.tabIndex).toBe(0);
        // Without it a touch drag scrolls the page instead of resizing.
        expect(s.style.touchAction).toBe("none");
    });

    it("resets on a double click", () => {
        const onChange = vi.fn();
        render(<Pane onChange={onChange} initial={560} />);
        act(() => {
            fireEvent.doubleClick(sep());
        });
        expect(onChange).toHaveBeenCalledWith(DEFAULT);
    });
});

describe("capturePointerDrag", () => {
    function Draggable({ onMove, onEnd }: { onMove: (e: PointerEvent) => void; onEnd?: () => void }) {
        return (
            <div
                data-testid="grip"
                onPointerDown={(e) => capturePointerDrag(e, { onMove, onEnd })}
            />
        );
    }

    it("tears the listeners down on pointerup", () => {
        const onMove = vi.fn();
        const onEnd = vi.fn();
        render(<Draggable onMove={onMove} onEnd={onEnd} />);
        const grip = screen.getByTestId("grip");

        fireEvent.pointerDown(grip, { pointerId: 3, button: 0 });
        fireEvent.pointerMove(grip, { pointerId: 3, clientX: 10 });
        expect(onMove).toHaveBeenCalledTimes(1);
        // The body lock is what lets the callers skip preventDefault on
        // pointerdown, which would suppress the mousedown every click-outside
        // listener in the app is registered on.
        expect(document.body.style.userSelect).toBe("none");

        fireEvent.pointerUp(grip, { pointerId: 3 });
        expect(onEnd).toHaveBeenCalledTimes(1);
        expect(document.body.style.userSelect).toBe("");

        fireEvent.pointerMove(grip, { pointerId: 3, clientX: 20 });
        expect(onMove).toHaveBeenCalledTimes(1);
    });

    it("tears them down on pointercancel too", () => {
        const onMove = vi.fn();
        render(<Draggable onMove={onMove} />);
        const grip = screen.getByTestId("grip");

        fireEvent.pointerDown(grip, { pointerId: 4, button: 0 });
        // A touch the browser took over never sends pointerup, so without this
        // the listeners outlive the gesture for the rest of the session.
        fireEvent.pointerCancel(grip, { pointerId: 4 });
        fireEvent.pointerMove(grip, { pointerId: 4, clientX: 30 });
        expect(onMove).not.toHaveBeenCalled();
    });

    it("releases the page even when the handle is unmounted mid-drag", () => {
        const onEnd = vi.fn();
        const { unmount } = render(<Draggable onMove={vi.fn()} onEnd={onEnd} />);
        const grip = screen.getByTestId("grip");

        fireEvent.pointerDown(grip, { pointerId: 7, button: 0 });
        expect(document.body.style.userSelect).toBe("none");

        // The listeners live on the element, so they go with it; without a net
        // on window the whole app stays unselectable for the session.
        unmount();
        fireEvent.pointerUp(window, { pointerId: 7 });
        expect(document.body.style.userSelect).toBe("");
        expect(onEnd).toHaveBeenCalledTimes(1);
    });

    it("ignores a second finger", () => {
        const onMove = vi.fn();
        render(<Draggable onMove={onMove} />);
        const grip = screen.getByTestId("grip");

        fireEvent.pointerDown(grip, { pointerId: 5, button: 0 });
        fireEvent.pointerMove(grip, { pointerId: 6, clientX: 40 });
        expect(onMove).not.toHaveBeenCalled();
        fireEvent.pointerMove(grip, { pointerId: 5, clientX: 40 });
        expect(onMove).toHaveBeenCalledTimes(1);
    });
});
