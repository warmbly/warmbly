import React from "react";
import { CalendarIcon, XIcon } from "lucide-react";
import Calendar from "../../Calendar";
import { format } from "date-fns";
import { Label } from "@/components/ui/field";

export default function DateSelect({
    title,
    value,
    onChange,
}: {
    title: string;
    value: Date | null;
    onChange: (v: Date | null) => void;
}) {
    const [open, setOpen] = React.useState(false);
    const modalRef = React.useRef<HTMLDivElement>(null);

    React.useEffect(() => {
        const handleClickOutside = (event: MouseEvent) => {
            if (modalRef.current && !modalRef.current.contains(event.target as Node)) {
                setOpen(false);
            }
        };
        if (open) {
            document.addEventListener("mousedown", handleClickOutside);
        }
        return () => {
            document.removeEventListener("mousedown", handleClickOutside);
        };
    }, [open]);

    return (
        <div className="relative w-full">
            <div className="flex items-center gap-1.5 mb-1.5">
                <Label className="mb-0">{title}</Label>
                {value && (
                    <button
                        type="button"
                        onClick={() => onChange(null)}
                        aria-label={`Clear ${title}`}
                        className="size-4 rounded text-slate-400 hover:text-rose-500 hover:bg-rose-50 inline-flex items-center justify-center transition-colors"
                    >
                        <XIcon className="w-3 h-3" />
                    </button>
                )}
            </div>
            <div className="relative select-none" ref={modalRef}>
                <button
                    type="button"
                    onClick={() => setOpen(!open)}
                    className={`flex gap-2 items-center h-7 px-2.5 w-full rounded-md border text-[12.5px] transition-colors ${
                        open
                            ? "border-sky-400 ring-2 ring-sky-100 text-slate-900"
                            : "border-slate-200 text-slate-700 hover:border-slate-300"
                    }`}
                >
                    <CalendarIcon className="w-3.5 h-3.5 text-slate-400 shrink-0" />
                    {value ? (
                        <span className="tabular-nums">{format(value, "dd MMM, yyyy")}</span>
                    ) : (
                        <span className="text-slate-400">Not set</span>
                    )}
                </button>
                <Calendar date={value} active={open} close={() => setOpen(false)} onSubmit={onChange} />
            </div>
        </div>
    );
}
