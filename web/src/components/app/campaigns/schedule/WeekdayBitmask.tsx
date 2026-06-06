// Active-days picker — a roomier grid of 7 day cells backed by a uint8
// day-of-week bitmask (bit i = weekday i, 0=Mon..6=Sun). On-theme: sky
// active, slate idle, h-12 rounded-md cells with a 3-letter label over a
// status dot. Logic is identical to the bitmask contract — only the cell
// presentation changed.

export default function WeekdayBitmask({
    weekdays,
    value,
    setValue,
}: {
    weekdays: string[];
    value: number;
    setValue: (v: number) => void;
}) {
    return (
        <div className="grid grid-cols-7 gap-1.5">
            {weekdays.map((day, index) => {
                const mask = 1 << index;
                const active = (value & mask) !== 0;
                return (
                    <button
                        key={day}
                        type="button"
                        aria-pressed={active}
                        title={day}
                        onClick={() => setValue(value ^ mask)}
                        className={`h-12 rounded-md border flex flex-col items-center justify-center transition-colors ${
                            active
                                ? "border-sky-500 bg-sky-50 text-sky-700"
                                : "border-slate-200 bg-white text-slate-500 hover:border-slate-300"
                        }`}
                    >
                        <span className="text-[11px] font-medium">{day.slice(0, 3)}</span>
                        <span
                            className={`mt-1 size-1.5 rounded-full ${
                                active ? "bg-sky-500" : "bg-slate-300"
                            }`}
                        />
                    </button>
                );
            })}
        </div>
    );
}
