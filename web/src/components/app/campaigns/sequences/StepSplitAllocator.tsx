// The traffic-split control for a step's A/B arms, as one compact line. Every
// arm (the Original control plus each active variant) is a draggable segment of
// a single bar: drag a divider to shift share between two neighbours, click a
// segment to edit that arm's content below. Add a variant inline with the +
// button. Per-variant actions (rename, pause, delete) live in the selected
// arm's editor, so this stays a single row instead of a tall list. Weights are
// stored as the percentages shown here; the sender normalizes them at send time
// (see internal/app/advanced SelectVariant).

import React from "react";
import { PlusIcon, Loader2Icon, PauseIcon } from "lucide-react";

export interface SplitArm {
    key: string; // "original" or the variant id
    name: string;
    weight: number; // current persisted weight
    active: boolean;
    isOriginal: boolean;
    winner?: boolean;
}

// Distinct accent per arm position. Original is always first (sky).
const ARM = ["bg-sky-500", "bg-violet-500", "bg-emerald-500", "bg-amber-500", "bg-rose-500", "bg-cyan-500"];
const colorOf = (i: number) => ARM[i % ARM.length];

const MIN = 1; // a sending arm must keep at least 1% (DB CHECK weight > 0)

// Round a weight map to integer percents that sum to exactly 100.
function toPercents(arms: SplitArm[]): Record<string, number> {
    if (arms.length === 0) return {};
    const total = arms.reduce((s, a) => s + Math.max(a.weight, MIN), 0);
    if (total <= 0) return Object.fromEntries(arms.map((a) => [a.key, Math.round(100 / arms.length)]));
    const raw = arms.map((a) => ({ key: a.key, exact: (Math.max(a.weight, MIN) / total) * 100 }));
    const out: Record<string, number> = {};
    let used = 0;
    for (const r of raw) {
        out[r.key] = Math.max(MIN, Math.floor(r.exact));
        used += out[r.key];
    }
    let leftover = 100 - used;
    const order = [...raw].sort((a, b) => b.exact - a.exact);
    let i = 0;
    while (leftover > 0 && order.length) {
        out[order[i % order.length].key] += 1;
        leftover -= 1;
        i += 1;
    }
    return out;
}

export default function StepSplitAllocator({
    arms,
    selectedKey,
    onSelect,
    onCommit,
    onAdd,
    onEven,
    canAdd,
    adding,
    busy,
}: {
    arms: SplitArm[];
    selectedKey: string;
    onSelect: (key: string) => void;
    onCommit: (weights: Record<string, number>) => void;
    onAdd: () => void;
    onEven: () => void;
    canAdd: boolean;
    adding: boolean;
    busy: boolean;
}) {
    const active = arms.filter((a) => a.active);
    const paused = arms.filter((a) => !a.active);
    const colorIndex = React.useMemo(() => {
        const m = new Map<string, number>();
        arms.forEach((a, i) => m.set(a.key, i));
        return m;
    }, [arms]);

    // Local percent draft, reseeded from props whenever the persisted split
    // changes (after a commit invalidates, or a teammate edits).
    const signature = active.map((a) => `${a.key}:${a.weight}`).join("|");
    const [pct, setPct] = React.useState<Record<string, number>>(() => toPercents(active));
    React.useEffect(() => {
        setPct(toPercents(active));
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [signature]);

    const barRef = React.useRef<HTMLDivElement>(null);
    const dragRef = React.useRef<{ leftKey: string; rightKey: string; startX: number; width: number; sum: number; left0: number } | null>(null);

    const onHandleDown = (leftKey: string, rightKey: string) => (e: React.PointerEvent) => {
        e.preventDefault();
        e.stopPropagation();
        const width = barRef.current?.getBoundingClientRect().width ?? 1;
        dragRef.current = { leftKey, rightKey, startX: e.clientX, width, sum: (pct[leftKey] ?? 0) + (pct[rightKey] ?? 0), left0: pct[leftKey] ?? 0 };
        (e.target as HTMLElement).setPointerCapture(e.pointerId);
    };
    const onHandleMove = (e: React.PointerEvent) => {
        const d = dragRef.current;
        if (!d) return;
        const deltaPct = ((e.clientX - d.startX) / d.width) * 100;
        let left = Math.round(d.left0 + deltaPct);
        left = Math.min(d.sum - MIN, Math.max(MIN, left));
        setPct((p) => ({ ...p, [d.leftKey]: left, [d.rightKey]: d.sum - left }));
    };
    const onHandleUp = (e: React.PointerEvent) => {
        if (!dragRef.current) return;
        (e.target as HTMLElement).releasePointerCapture?.(e.pointerId);
        dragRef.current = null;
        onCommit(pct);
    };

    // Cumulative left offsets for the drag handles between active segments.
    let cum = 0;
    const handles: { left: number; leftKey: string; rightKey: string }[] = [];
    active.forEach((a, i) => {
        cum += pct[a.key] ?? 0;
        if (i < active.length - 1) handles.push({ left: cum, leftKey: a.key, rightKey: active[i + 1].key });
    });

    return (
        <div className="border-b border-slate-200">
            <div className="flex items-center gap-2 px-2 py-2">
                {/* The split bar: one segment per active arm, draggable dividers. */}
                <div ref={barRef} className="relative h-8 flex-1 min-w-0 select-none rounded-md overflow-hidden ring-1 ring-inset ring-slate-200 flex">
                    {active.map((a) => {
                        const p = pct[a.key] ?? 0;
                        return (
                            <button
                                key={a.key}
                                type="button"
                                onClick={() => onSelect(a.key)}
                                style={{ width: `${p}%` }}
                                className={`${colorOf(colorIndex.get(a.key) ?? 0)} h-full min-w-0 flex items-center justify-center text-white transition-[width] duration-75 ${
                                    selectedKey === a.key ? "ring-2 ring-inset ring-white/70" : "hover:brightness-110"
                                }`}
                                title={`${a.name} — ${p}%`}
                            >
                                {p >= 10 && (
                                    <span className="px-1 truncate text-[11px] font-medium tabular-nums">
                                        {a.name} <span className="opacity-80">{p}%</span>
                                    </span>
                                )}
                            </button>
                        );
                    })}
                    {handles.map((h) => (
                        <div
                            key={`${h.leftKey}->${h.rightKey}`}
                            onPointerDown={onHandleDown(h.leftKey, h.rightKey)}
                            onPointerMove={onHandleMove}
                            onPointerUp={onHandleUp}
                            style={{ left: `${h.left}%` }}
                            className="group absolute top-0 bottom-0 z-10 flex w-3 -ml-1.5 cursor-col-resize items-center justify-center"
                        >
                            <span className="h-4 w-[3px] rounded-full bg-white/80 shadow ring-1 ring-slate-300 group-hover:h-full group-hover:bg-white" />
                        </div>
                    ))}
                </div>

                {busy && <Loader2Icon className="w-3.5 h-3.5 shrink-0 animate-spin text-slate-300" />}
                <button
                    type="button"
                    onClick={onEven}
                    disabled={active.length < 2}
                    title="Even split"
                    className="h-8 shrink-0 px-2 rounded-md text-[12px] font-medium text-slate-500 hover:bg-slate-100 hover:text-slate-900 disabled:opacity-40"
                >
                    Even
                </button>
                <button
                    type="button"
                    onClick={onAdd}
                    disabled={!canAdd || adding}
                    title="Add an A/B variant"
                    className="h-8 shrink-0 px-2 inline-flex items-center gap-1 rounded-md bg-sky-600 text-[12px] font-medium text-white hover:bg-sky-700 disabled:opacity-50"
                >
                    {adding ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <PlusIcon className="w-3.5 h-3.5" />}
                    Variant
                </button>
            </div>

            {/* Paused variants are out of the split; surface them so they stay reachable. */}
            {paused.length > 0 && (
                <div className="flex flex-wrap items-center gap-1.5 px-2 pb-2 text-[11px] text-slate-400">
                    <PauseIcon className="w-3 h-3" />
                    <span>Paused</span>
                    {paused.map((a) => (
                        <button
                            key={a.key}
                            type="button"
                            onClick={() => onSelect(a.key)}
                            className={`rounded-md px-1.5 py-0.5 text-[11px] font-medium transition-colors ${
                                selectedKey === a.key ? "bg-slate-200 text-slate-700" : "bg-slate-100 text-slate-500 hover:bg-slate-200 hover:text-slate-700"
                            }`}
                        >
                            {a.name}
                        </button>
                    ))}
                </div>
            )}
        </div>
    );
}
