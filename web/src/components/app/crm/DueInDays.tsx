// DueInDays — a relative "due in N days" control used wherever a task due date
// is set (the unibox thread panel + the tasks-tab dialog). People think in
// "follow up in 3 days", not absolute calendar dates, so we capture a day
// offset and convert to an ISO timestamp at the boundary. `null` = no due date.

import React from "react";
import { CalendarClockIcon, XIcon } from "lucide-react";
import { NumberInput } from "@/components/ui/field";

const PRESETS = [
    { label: "Today", days: 0 },
    { label: "Tomorrow", days: 1 },
    { label: "3d", days: 3 },
    { label: "1w", days: 7 },
];

export default function DueInDays({
    value,
    onChange,
    defaultDays = 3,
}: {
    value: number | null;
    onChange: (days: number | null) => void;
    defaultDays?: number;
}) {
    if (value === null) {
        return (
            <button
                type="button"
                onClick={() => onChange(defaultDays)}
                className="h-7 px-2.5 rounded-md border border-dashed border-slate-300 text-[12px] text-slate-500 hover:text-slate-900 hover:border-slate-400 inline-flex items-center gap-1.5 transition-colors"
            >
                <CalendarClockIcon className="w-3 h-3" />
                Set due date
            </button>
        );
    }

    return (
        <div className="flex items-center gap-2 flex-wrap">
            <div className="inline-flex items-center gap-1.5">
                <span className="text-[11px] text-slate-400">Due in</span>
                <NumberInput
                    value={value}
                    onChange={(n) => onChange(Math.max(0, Math.round(n)))}
                    min={0}
                    className="w-[64px]"
                />
                <span className="text-[11px] text-slate-400">{value === 1 ? "day" : "days"}</span>
                <button
                    type="button"
                    onClick={() => onChange(null)}
                    aria-label="Clear due date"
                    className="size-5 rounded text-slate-400 hover:text-slate-700 inline-flex items-center justify-center"
                >
                    <XIcon className="w-3 h-3" />
                </button>
            </div>
            <div className="flex items-center gap-0.5">
                {PRESETS.map((p) => (
                    <button
                        key={p.days}
                        type="button"
                        onClick={() => onChange(p.days)}
                        className={`h-6 px-1.5 rounded text-[10.5px] transition-colors ${
                            value === p.days
                                ? "bg-sky-50 text-sky-700 font-medium"
                                : "text-slate-500 hover:text-slate-900"
                        }`}
                    >
                        {p.label}
                    </button>
                ))}
            </div>
        </div>
    );
}
