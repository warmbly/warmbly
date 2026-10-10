import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import EmailBody from "./EmailBody";

let resize: ResizeObserverCallback;
const disconnect = vi.fn();

beforeEach(() => {
    vi.useFakeTimers();
    disconnect.mockClear();
    vi.stubGlobal("ResizeObserver", class {
        constructor(callback: ResizeObserverCallback) { resize = callback; }
        observe = vi.fn();
        disconnect = disconnect;
    });
});

afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
});

function layout(container: HTMLElement, initialWidth = 400, initialHeight = 1000, initialContentWidth = 0) {
    const frame = container.querySelector("iframe")!;
    const viewport = frame.parentElement!;
    const doc = frame.contentDocument!;
    if (!doc.documentElement) doc.append(doc.createElement("html"));
    const size = { width: initialWidth, height: initialHeight, contentWidth: initialContentWidth };
    Object.defineProperty(viewport, "clientWidth", { configurable: true, get: () => size.width });
    Object.defineProperty(doc.documentElement, "scrollWidth", {
        get: () => Math.max(size.contentWidth, parseFloat(frame.style.width) || 0),
    });
    Object.defineProperty(doc.documentElement, "scrollHeight", {
        get: () => Math.max(size.height, parseFloat(frame.style.height) || 0),
    });
    const rootStyle = vi.spyOn(doc.documentElement.style, "setProperty");
    fireEvent.load(frame);
    const update = (body = true) => act(() => {
        resize([{
            target: body ? doc.body : viewport,
            contentRect: new DOMRect(),
            borderBoxSize: [], contentBoxSize: [], devicePixelContentBoxSize: [],
        }], {} as ResizeObserver);
        vi.runOnlyPendingTimers();
    });
    return { frame, viewport, doc, size, update, rootStyle };
}

describe("EmailBody layout", () => {
    it("uses the full document height without making the iframe another scroll box", () => {
        const { container } = render(<EmailBody html="<p>A long message</p>" />);
        const { frame, viewport, doc } = layout(container);
        expect(frame.style.height).toBe("1000px");
        expect(viewport.style.height).toBe("1000px");
        expect(frame.style.transform).toBe("");
        expect(frame.getAttribute("scrolling")).toBe("no");
        expect(doc.documentElement.style.overflow).toBe("hidden");
        expect(frame.getAttribute("sandbox")).not.toContain("allow-scripts");
    });

    it("shrinks after content collapses instead of retaining the previous viewport height", () => {
        const { container } = render(<EmailBody html="<p>A message</p>" />);
        const { frame, viewport, size, update } = layout(container);
        size.height = 120;
        update();
        expect(frame.style.height).toBe("120px");
        expect(viewport.style.height).toBe("120px");
    });

    it("fits an unwrappable newsletter and recalculates when the reading pane widens", () => {
        const { container } = render(<EmailBody html='<table width="800"><tr><td>Newsletter</td></tr></table>' />);
        const { frame, viewport, size, update } = layout(container, 400, 1200, 800);
        expect(frame.style.width).toBe("800px");
        expect(frame.style.transform).toBe("scale(0.5)");
        expect(viewport.style.height).toBe("600px");
        size.width = 1000;
        size.height = 900;
        update(false);
        expect(frame.style.width).toBe("1000px");
        expect(frame.style.transform).toBe("");
        expect(viewport.style.height).toBe("900px");
    });

    it("remeasures when an image loads late", () => {
        const { container } = render(<EmailBody html='<img src="data:image/png;base64,AAAA">' />);
        const { frame, doc, size } = layout(container, 400, 100);
        size.height = 650;
        act(() => {
            fireEvent.load(doc.querySelector("img")!);
            vi.runOnlyPendingTimers();
        });
        expect(frame.style.height).toBe("650px");
    });

    it("overrides sender root dimensions and scrolling without dropping the message styling", () => {
        const { container } = render(<EmailBody html='<html style="overflow:scroll!important;width:900px!important"><body style="height:500px!important;overflow:auto!important;background:white"><h1>Digest</h1></body></html>' />);
        const { doc, rootStyle } = layout(container);
        expect(doc.documentElement.style.overflow).toBe("hidden");
        expect(rootStyle).toHaveBeenCalledWith("overflow", "hidden", "important");
        expect(doc.documentElement.style.width).toBe("auto");
        expect(doc.body.style.height).toBe("auto");
        expect(doc.body.style.overflow).toBe("visible");
        expect(doc.body.style.background).toBe("white");
        expect(doc.querySelector("h1")?.textContent).toBe("Digest");
    });

    it("disconnects the previous document observer when quoted history toggles", () => {
        const { container } = render(<EmailBody html='<p>Latest reply</p><div class="gmail_quote">History</div>' />);
        layout(container);
        fireEvent.click(screen.getByRole("button", { name: "Show quoted text" }));
        layout(container);
        expect(disconnect).toHaveBeenCalledTimes(1);
        expect(screen.getByRole("button", { name: "Hide quoted text" })).toHaveAttribute("aria-expanded", "true");
    });
});
