import { warmupDaysLabel } from "@/lib/warmupSchedule";
import WeekdayBitmask from "@/components/app/campaigns/schedule/WeekdayBitmask";

const WEEKDAYS = ["Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"];

export default function WarmupDaysPicker({ value, onChange, inheritedDays, allowFollow = true }: {
    value: number;
    onChange: (days: number) => void;
    inheritedDays?: number;
    allowFollow?: boolean;
}) {
    const effectiveDays = value || inheritedDays;
    return (
        <div className="space-y-2.5">
            <div className="flex flex-wrap gap-1.5" role="group" aria-label="Warmup day presets">
                {[
                    { days: 0, label: "Follow sending plan" },
                    { days: 127, label: "Every day" },
                    { days: 31, label: "Weekdays" },
                ].filter(({ days }) => allowFollow || days !== 0).map(({ days, label }) => (
                    <button
                        key={days}
                        type="button"
                        aria-pressed={value === days}
                        onClick={() => onChange(days)}
                        className={`rounded-md border px-2.5 py-1.5 text-[11.5px] font-medium transition-colors ${value === days ? "border-sky-500 bg-sky-50 text-sky-700" : "border-slate-200 text-slate-600 hover:border-slate-300"}`}
                    >
                        {label}
                    </button>
                ))}
            </div>
            {effectiveDays !== undefined && (
                <WeekdayBitmask
                    weekdays={WEEKDAYS}
                    value={effectiveDays}
                    setValue={(days) => { if (days !== 0) onChange(days); }}
                />
            )}
            <p className="text-[11.5px] text-slate-600 leading-relaxed" aria-live="polite">
                {value === 0
                    ? inheritedDays === undefined
                        ? "Uses sending-plan days, or every day with Fixed schedule."
                        : `Following sending plan: ${warmupDaysLabel(inheritedDays)}.`
                    : `Warmup days: ${warmupDaysLabel(value)}. Every selected day applies, including weekends. Campaign working days stay separate.`}
            </p>
            <p className="text-[11px] text-slate-400 leading-relaxed">
                Select at least one day. Sending-plan hours and breaks still apply while the plan is on.
            </p>
        </div>
    );
}
