// A horizontal strip (tab bar, chip row) that scrolls when it overflows: edge
// fades and chevrons say there is more, the wheel scrolls it sideways, and the
// child marked data-active="true" is kept in view.
import React, { useCallback, useEffect, useRef, useState } from "react";
import { ChevronLeftIcon, ChevronRightIcon } from "lucide-react";
import { cn } from "@/lib/utils";

export default function ScrollStrip({
    children,
    activeKey,
    className,
    innerClassName,
    fade = "from-white",
}: {
    children: React.ReactNode;
    /** Changing it scrolls the data-active child into view. */
    activeKey?: string;
    className?: string;
    innerClassName?: string;
    /** Tailwind gradient start matching the strip's background. */
    fade?: string;
}) {
    const ref = useRef<HTMLDivElement | null>(null);
    const [edges, setEdges] = useState({ left: false, right: false });

    const measure = useCallback(() => {
        const el = ref.current;
        if (!el) return;
        const left = el.scrollLeft > 1;
        const right = el.scrollLeft + el.clientWidth < el.scrollWidth - 1;
        setEdges((cur) => (cur.left === left && cur.right === right ? cur : { left, right }));
    }, []);

    useEffect(() => {
        const el = ref.current;
        if (!el) return;
        measure();
        const ro = new ResizeObserver(measure);
        ro.observe(el);
        for (const child of Array.from(el.children)) ro.observe(child);
        // A vertical wheel over an overflowing strip scrolls it sideways.
        const onWheel = (e: WheelEvent) => {
            if (el.scrollWidth <= el.clientWidth || Math.abs(e.deltaX) > Math.abs(e.deltaY)) return;
            // At the end it cannot move toward, the wheel scrolls the page instead.
            const atStart = el.scrollLeft <= 0 && e.deltaY < 0;
            const atEnd = el.scrollLeft + el.clientWidth >= el.scrollWidth - 1 && e.deltaY > 0;
            if (atStart || atEnd) return;
            e.preventDefault();
            el.scrollLeft += e.deltaY;
        };
        el.addEventListener("wheel", onWheel, { passive: false });
        return () => {
            ro.disconnect();
            el.removeEventListener("wheel", onWheel);
        };
    }, [measure]);

    // Scrolls the strip itself only; scrollIntoView would also scroll the page.
    useEffect(() => {
        const el = ref.current;
        const active = el?.querySelector<HTMLElement>('[data-active="true"]');
        if (!el || !active) return;
        const strip = el.getBoundingClientRect();
        const item = active.getBoundingClientRect();
        const pad = 36;
        if (item.left < strip.left + pad) el.scrollBy({ left: item.left - strip.left - pad, behavior: "smooth" });
        else if (item.right > strip.right - pad) el.scrollBy({ left: item.right - strip.right + pad, behavior: "smooth" });
    }, [activeKey]);

    const nudge = (dir: -1 | 1) => {
        const el = ref.current;
        if (el) el.scrollBy({ left: dir * el.clientWidth * 0.6, behavior: "smooth" });
    };

    return (
        <div className={cn("relative min-w-0", className)}>
            <div ref={ref} onScroll={measure} className={cn("flex items-center overflow-x-auto overflow-y-hidden no-scrollbar", innerClassName)}>
                {children}
            </div>
            {edges.left && (
                <button
                    type="button"
                    aria-label="Scroll left"
                    onClick={() => nudge(-1)}
                    className={cn("absolute left-0 inset-y-0 w-9 flex items-center justify-start pl-1 bg-gradient-to-r to-transparent text-slate-400 hover:text-slate-700", fade)}
                >
                    <ChevronLeftIcon className="w-3.5 h-3.5" />
                </button>
            )}
            {edges.right && (
                <button
                    type="button"
                    aria-label="Scroll right"
                    onClick={() => nudge(1)}
                    className={cn("absolute right-0 inset-y-0 w-9 flex items-center justify-end pr-1 bg-gradient-to-l to-transparent text-slate-400 hover:text-slate-700", fade)}
                >
                    <ChevronRightIcon className="w-3.5 h-3.5" />
                </button>
            )}
        </div>
    );
}
