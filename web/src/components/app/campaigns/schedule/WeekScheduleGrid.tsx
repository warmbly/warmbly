// Calendar-style weekly sending editor with FULL per-day flexibility. Each
// Mon–Sun column owns an independent list of sending windows:
//   • drag on empty space in a column to draw a new window
//   • drag a window's body to move it, or its top/bottom edge to resize
//   • × removes a window; the ⧉ in a day header copies that day to every day
// Everything snaps to 30-min steps. Windows here are in DISPLAY order (index
// 0 = Monday); the page converts to/from the Sun=0 wire format.

import React from "react";
import { CopyIcon, PlusIcon, XIcon } from "lucide-react";

export interface Interval {
    start: number; // minutes since midnight [0,1440)
    end: number; // minutes since midnight (start,1440]
}

const ABBR = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];
const HOURS = [0, 3, 6, 9, 12, 15, 18, 21, 24];
const BODY_H = 420;
const GUTTER = 46;
const SNAP = 30;
const MIN_DRAW = 60;
const DAY = 1440;

const clamp = (n: number, lo: number, hi: number) => Math.max(lo, Math.min(hi, n));
const snap = (n: number) => Math.round(n / SNAP) * SNAP;
const pct = (min: number) => `${(min / DAY) * 100}%`;
const hourLabel = (h: number) => {
    const hh = h % 24;
    const ampm = hh < 12 ? "a" : "p";
    const h12 = hh % 12 === 0 ? 12 : hh % 12;
    return `${h12}${ampm}`;
};
const fmt = (min: number) => {
    const h = Math.floor(min / 60);
    const m = min % 60;
    const ampm = h < 12 ? "am" : "pm";
    const h12 = h % 12 === 0 ? 12 : h % 12;
    return m === 0 ? `${h12}${ampm}` : `${h12}:${String(m).padStart(2, "0")}${ampm}`;
};

function mergeIntervals(ivs: Interval[]): Interval[] {
    const sorted = ivs.filter((iv) => iv.end > iv.start).sort((a, b) => a.start - b.start);
    const out: Interval[] = [];
    for (const iv of sorted) {
        const last = out[out.length - 1];
        if (last && iv.start <= last.end) last.end = Math.max(last.end, iv.end);
        else out.push({ ...iv });
    }
    return out;
}

type DragMode = "move" | "start" | "end" | "draw";
interface DragState {
    day: number;
    idx: number;
    mode: DragMode;
    startY: number;
    anchorMin: number; // draw: the fixed edge
    origStart: number;
    origEnd: number;
    rectTop: number;
    rectH: number;
}

export default function WeekScheduleGrid({
    windows,
    onChange,
}: {
    windows: Interval[][]; // length 7, display order (Mon=0)
    onChange: (next: Interval[][]) => void;
}) {
    const todayIdx = (new Date().getDay() + 6) % 7; // Mon=0..Sun=6
    const [drag, setDrag] = React.useState<DragState | null>(null);

    // Refs keep the move/up listeners reading the freshest state without
    // re-subscribing mid-gesture.
    const winRef = React.useRef(windows);
    winRef.current = windows;
    const changeRef = React.useRef(onChange);
    changeRef.current = onChange;

    const setDay = (day: number, next: Interval[]) =>
        changeRef.current(winRef.current.map((d, i) => (i === day ? next : d)));
    const setInterval = (day: number, idx: number, iv: Interval) =>
        setDay(
            day,
            winRef.current[day].map((x, i) => (i === idx ? iv : x)),
        );

    React.useEffect(() => {
        if (!drag) return;
        const yToMin = (clientY: number) => clamp(((clientY - drag.rectTop) / drag.rectH) * DAY, 0, DAY);
        const move = (e: PointerEvent) => {
            if (drag.mode === "draw") {
                const cur = snap(yToMin(e.clientY));
                const s = clamp(Math.min(drag.anchorMin, cur), 0, DAY - SNAP);
                let en = clamp(Math.max(drag.anchorMin, cur), s + SNAP, DAY);
                if (en - s < SNAP) en = s + SNAP;
                setInterval(drag.day, drag.idx, { start: s, end: en });
                return;
            }
            const deltaMin = ((e.clientY - drag.startY) / drag.rectH) * DAY;
            const dur = drag.origEnd - drag.origStart;
            if (drag.mode === "move") {
                const s = clamp(snap(drag.origStart + deltaMin), 0, DAY - dur);
                setInterval(drag.day, drag.idx, { start: s, end: s + dur });
            } else if (drag.mode === "start") {
                const s = clamp(snap(drag.origStart + deltaMin), 0, drag.origEnd - SNAP);
                setInterval(drag.day, drag.idx, { start: s, end: drag.origEnd });
            } else {
                const en = clamp(snap(drag.origEnd + deltaMin), drag.origStart + SNAP, DAY);
                setInterval(drag.day, drag.idx, { start: drag.origStart, end: en });
            }
        };
        const up = () => {
            let dayIvs = winRef.current[drag.day];
            if (drag.mode === "draw") {
                const iv = dayIvs[drag.idx];
                if (iv && iv.end - iv.start < MIN_DRAW) {
                    let s = iv.start;
                    const en = Math.min(iv.start + MIN_DRAW, DAY);
                    if (en - s < MIN_DRAW) s = en - MIN_DRAW;
                    dayIvs = dayIvs.map((x, i) => (i === drag.idx ? { start: s, end: en } : x));
                }
            }
            setDay(drag.day, mergeIntervals(dayIvs));
            setDrag(null);
        };
        window.addEventListener("pointermove", move);
        window.addEventListener("pointerup", up);
        const prevCursor = document.body.style.cursor;
        document.body.style.cursor = drag.mode === "move" ? "grabbing" : "ns-resize";
        document.body.style.userSelect = "none";
        return () => {
            window.removeEventListener("pointermove", move);
            window.removeEventListener("pointerup", up);
            document.body.style.cursor = prevCursor;
            document.body.style.userSelect = "";
        };
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [drag]);

    const beginBlockDrag = (e: React.PointerEvent, day: number, idx: number, mode: DragMode) => {
        e.preventDefault();
        e.stopPropagation();
        const col = e.currentTarget.closest("[data-col-body]") as HTMLElement | null;
        const rect = (col ?? (e.currentTarget as HTMLElement)).getBoundingClientRect();
        const iv = windows[day][idx];
        setDrag({
            day,
            idx,
            mode,
            startY: e.clientY,
            anchorMin: 0,
            origStart: iv.start,
            origEnd: iv.end,
            rectTop: rect.top,
            rectH: rect.height,
        });
    };

    const beginDraw = (e: React.PointerEvent, day: number) => {
        // Touch/pen: do NOT draw — let the column scroll the page. Add via the
        // "+" in the day header instead. Drawing-by-drag stays a mouse gesture.
        if (e.pointerType !== "mouse") return;
        // Only the empty column surface starts a draw (blocks stopPropagation).
        e.preventDefault();
        const rect = e.currentTarget.getBoundingClientRect();
        const anchor = clamp(snap(((e.clientY - rect.top) / rect.height) * DAY), 0, DAY - SNAP);
        const newIv: Interval = { start: anchor, end: anchor + SNAP };
        const idx = winRef.current[day].length;
        setDay(day, [...winRef.current[day], newIv]);
        setDrag({
            day,
            idx,
            mode: "draw",
            startY: e.clientY,
            anchorMin: anchor,
            origStart: anchor,
            origEnd: anchor + SNAP,
            rectTop: rect.top,
            rectH: rect.height,
        });
    };

    const removeInterval = (day: number, idx: number) =>
        setDay(day, windows[day].filter((_, i) => i !== idx));
    // Touch-friendly add (no drag needed): drop a default 9–5 window, then drag
    // the block to adjust.
    const addDefault = (day: number) =>
        setDay(day, mergeIntervals([...winRef.current[day], { start: 9 * 60, end: 17 * 60 }]));
    const copyToAll = (day: number) => {
        const src = windows[day].map((iv) => ({ ...iv }));
        onChange(windows.map(() => src.map((iv) => ({ ...iv }))));
    };

    return (
        <div className="min-w-[520px]">
            {/* day headers */}
            <div className="flex">
                <div style={{ width: GUTTER }} className="shrink-0" />
                <div className="flex flex-1 gap-px">
                    {ABBR.map((d, i) => {
                        const active = windows[i].length > 0;
                        return (
                            <div
                                key={d}
                                className={`group/h relative flex-1 h-9 flex items-center justify-center rounded-t-md text-[11px] ${
                                    active ? "text-sky-700 font-semibold bg-sky-50/60" : "text-slate-400"
                                }`}
                            >
                                {d}
                                {i === todayIdx && (
                                    <span className="absolute bottom-1 size-1 rounded-full bg-sky-500" title="Today" />
                                )}
                                <button
                                    type="button"
                                    title="Add a window"
                                    onClick={() => addDefault(i)}
                                    className="absolute left-1 top-1/2 -translate-y-1/2 md:top-1.5 md:translate-y-0 inline-flex size-6 md:size-4 items-center justify-center rounded text-slate-400 transition-opacity hover:bg-white hover:text-sky-600 opacity-100 md:opacity-0 md:group-hover/h:opacity-100"
                                >
                                    <PlusIcon className="w-2.5 h-2.5" />
                                </button>
                                {active && (
                                    <button
                                        type="button"
                                        title="Copy this day to every day"
                                        onClick={() => copyToAll(i)}
                                        className="absolute right-1 top-1/2 -translate-y-1/2 md:top-1.5 md:translate-y-0 size-6 md:size-4 rounded text-slate-400 hover:text-sky-600 hover:bg-white inline-flex items-center justify-center opacity-100 md:opacity-0 md:group-hover/h:opacity-100 transition-opacity"
                                    >
                                        <CopyIcon className="w-2.5 h-2.5" />
                                    </button>
                                )}
                            </div>
                        );
                    })}
                </div>
            </div>

            {/* grid body */}
            <div className="relative select-none" style={{ height: BODY_H }}>
                {HOURS.map((h) => (
                    <span
                        key={`l${h}`}
                        className="absolute left-0 -translate-y-1/2 text-right text-[10px] text-slate-400 tabular-nums pr-2"
                        style={{ top: pct(h * 60), width: GUTTER }}
                    >
                        {hourLabel(h)}
                    </span>
                ))}
                {HOURS.map((h) => (
                    <div
                        key={`g${h}`}
                        className="absolute right-0 border-t border-slate-100"
                        style={{ left: GUTTER, top: pct(h * 60) }}
                    />
                ))}

                <div className="absolute top-0 bottom-0 right-0 flex gap-px" style={{ left: GUTTER }}>
                    {ABBR.map((d, i) => {
                        const active = windows[i].length > 0;
                        const isToday = i === todayIdx;
                        return (
                            <div
                                key={d}
                                data-col-body
                                onPointerDown={(e) => beginDraw(e, i)}
                                className={`relative flex-1 cursor-crosshair touch-pan-y ${
                                    active ? "bg-sky-50/30" : "bg-slate-50/40 hover:bg-slate-100/50"
                                } ${isToday ? "ring-1 ring-inset ring-sky-100" : ""}`}
                            >
                                {windows[i].length === 0 && (
                                    <span className="pointer-events-none absolute inset-x-0 top-1/2 -translate-y-1/2 text-center text-[9.5px] text-slate-300 leading-tight px-1">
                                        drag, or tap +
                                    </span>
                                )}
                                {windows[i].map((iv, idx) => (
                                    <Block
                                        key={idx}
                                        iv={iv}
                                        dragging={drag !== null}
                                        onBody={(e) => beginBlockDrag(e, i, idx, "move")}
                                        onTop={(e) => beginBlockDrag(e, i, idx, "start")}
                                        onBottom={(e) => beginBlockDrag(e, i, idx, "end")}
                                        onRemove={() => removeInterval(i, idx)}
                                    />
                                ))}
                            </div>
                        );
                    })}
                </div>
            </div>
        </div>
    );
}

function Block({
    iv,
    dragging,
    onBody,
    onTop,
    onBottom,
    onRemove,
}: {
    iv: Interval;
    dragging: boolean;
    onBody: (e: React.PointerEvent) => void;
    onTop: (e: React.PointerEvent) => void;
    onBottom: (e: React.PointerEvent) => void;
    onRemove: () => void;
}) {
    const tall = iv.end - iv.start >= 90;
    return (
        <div
            onPointerDown={onBody}
            className={`group absolute inset-x-[3px] rounded-md border border-sky-400/60 bg-sky-500/15 overflow-hidden touch-pan-x ${
                dragging ? "cursor-grabbing" : "cursor-grab"
            }`}
            style={{ top: pct(iv.start), height: pct(iv.end - iv.start) }}
        >
            <div
                onPointerDown={(e) => {
                    e.stopPropagation();
                    onTop(e);
                }}
                className="absolute -top-1 inset-x-0 h-3 cursor-ns-resize touch-pan-x"
            >
                <div className="absolute top-1 left-1/2 -translate-x-1/2 h-0.5 w-6 rounded-full bg-sky-400/70 opacity-100 md:opacity-0 md:group-hover:opacity-100 transition-opacity" />
            </div>
            <div
                onPointerDown={(e) => {
                    e.stopPropagation();
                    onBottom(e);
                }}
                className="absolute -bottom-1 inset-x-0 h-3 cursor-ns-resize touch-pan-x"
            >
                <div className="absolute bottom-1 left-1/2 -translate-x-1/2 h-0.5 w-6 rounded-full bg-sky-400/70 opacity-100 md:opacity-0 md:group-hover:opacity-100 transition-opacity" />
            </div>
            <button
                type="button"
                title="Remove window"
                onPointerDown={(e) => e.stopPropagation()}
                onClick={(e) => {
                    e.stopPropagation();
                    onRemove();
                }}
                className="absolute right-0.5 top-0.5 size-6 md:size-4 rounded text-sky-700/70 hover:text-rose-600 hover:bg-white/80 inline-flex items-center justify-center opacity-100 md:opacity-0 md:group-hover:opacity-100 transition-opacity"
            >
                <XIcon className="w-2.5 h-2.5" />
            </button>
            <span
                className={`pointer-events-none absolute left-1.5 ${tall ? "top-1" : "top-1/2 -translate-y-1/2"} text-[9.5px] font-medium text-sky-700 tabular-nums leading-tight`}
            >
                {fmt(iv.start)}
                {tall ? <br /> : "–"}
                {fmt(iv.end)}
            </span>
        </div>
    );
}
