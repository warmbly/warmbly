import { useState } from "react";
import { CalendarIcon } from "lucide-react";
import { DatePicker } from "@/components/ui/DatePicker";
import { PopoverMenu, PopoverMenuContent, PopoverMenuTrigger } from "@/components/ui/popover-menu";
import { COHORT_TIP, PRESETS, formatWindow, type CampaignPeriod, type DayWindow } from "@/lib/campaignPeriod";

export default function CampaignPeriodPicker({
    value,
    onChange,
    resolved,
}: {
    value: CampaignPeriod;
    onChange: (p: CampaignPeriod) => void;
    // The window the figures on screen cover, to prefill a custom range.
    resolved: DayWindow | null;
}) {
    const [open, setOpen] = useState(false);
    const [from, setFrom] = useState("");
    const [to, setTo] = useState("");

    const openCustom = (o: boolean) => {
        if (o) {
            const start = value.key === "custom" ? value : resolved;
            setFrom(start?.from ?? "");
            setTo(start?.to ?? "");
        }
        setOpen(o);
    };

    const problem = !from || !to ? "Pick a first and a last day." : from > to ? "The first day is after the last day." : null;
    const custom = value.key === "custom";

    const tab = (active: boolean) =>
        `h-6 px-2 rounded text-[11px] font-medium transition-colors inline-flex items-center gap-1 ${
            active ? "bg-white text-slate-900 shadow-sm" : "text-slate-500 hover:text-slate-900"
        }`;

    return (
        <div className="inline-flex items-center gap-0.5 rounded-md bg-slate-100 p-0.5" title={COHORT_TIP}>
            {PRESETS.map((p) => (
                <button
                    key={p.key}
                    type="button"
                    onClick={() => {
                        onChange({ key: p.key });
                    }}
                    className={tab(value.key === p.key)}
                >
                    {p.label}
                </button>
            ))}
            <PopoverMenu open={open} onOpenChange={openCustom} align="end">
                <PopoverMenuTrigger asChild>
                    <button type="button" className={tab(custom)}>
                        <CalendarIcon className="w-3 h-3" />
                        {custom ? formatWindow(value) : "Custom"}
                    </button>
                </PopoverMenuTrigger>
                <PopoverMenuContent minWidth={256}>
                    <div className="w-64 max-w-[calc(100vw-2rem)] p-2.5">
                        <div className="grid grid-cols-2 gap-2">
                            <div>
                                <span className="block text-[10px] uppercase tracking-[0.12em] font-medium text-slate-500 mb-1">
                                    From
                                </span>
                                <DatePicker value={from} onChange={setFrom} placeholder="First day" clearable={false} className="w-full" />
                            </div>
                            <div>
                                <span className="block text-[10px] uppercase tracking-[0.12em] font-medium text-slate-500 mb-1">
                                    To
                                </span>
                                <DatePicker value={to} onChange={setTo} placeholder="Last day" clearable={false} className="w-full" />
                            </div>
                        </div>
                        <p className="mt-2 text-[10.5px] text-slate-400 leading-snug">{COHORT_TIP}.</p>
                        {problem && <p className="mt-1.5 text-[11px] text-rose-600">{problem}</p>}
                        <div className="mt-2.5 flex items-center justify-end gap-1.5">
                            <button
                                type="button"
                                onClick={() => setOpen(false)}
                                className="h-7 px-2.5 rounded-md text-[12px] font-medium text-slate-600 hover:text-slate-900 hover:bg-slate-100"
                            >
                                Cancel
                            </button>
                            <button
                                type="button"
                                disabled={!!problem}
                                onClick={() => {
                                    onChange({ key: "custom", from, to });
                                    setOpen(false);
                                }}
                                className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium transition-colors disabled:opacity-50 disabled:hover:bg-sky-600"
                            >
                                Apply
                            </button>
                        </div>
                    </div>
                </PopoverMenuContent>
            </PopoverMenu>
        </div>
    );
}
