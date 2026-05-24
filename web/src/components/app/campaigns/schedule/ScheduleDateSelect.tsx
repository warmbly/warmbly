import React from "react";
import { RiCloseLine, RiCalendarScheduleLine } from "@remixicon/react";
import Calendar from "../../Calendar";
import { format } from "date-fns";

export default function DateSelect({
    title,
    value,
    onChange,
}: {
    title: string,
    value: Date | null,
    onChange: (v: Date | null) => void,
}) {
    const [open, setOpen] = React.useState<boolean>(false);
    const modalRef = React.useRef<HTMLDivElement>(null);

    React.useEffect(() => {
        const handleClickOutside = (event: MouseEvent) => {
            if (modalRef.current && !modalRef.current.contains(event.target as Node)) {
                setOpen(false);
            }
        };
        if (open) {
            document.addEventListener('mousedown', handleClickOutside);
        }

        return () => {
            document.removeEventListener('mousedown', handleClickOutside);
        };
    }, [open]);

    return (
        <div className="relative w-full">
            <h1 className="font-inter flex gap-1 items-center text-slate-600 text-[15px] px-2.5 mb-2">
                {title}
                <span
                    className={`transition p-0.5 rounded-full ${value ? "opacity-100 cursor-pointer hover:bg-red-100" : "opacity-0"}`}
                    onClick={() => onChange(null)}
                >
                    <RiCloseLine className="w-3 h-3 text-red-400" />
                </span>
            </h1>
            <div className="relative select-none" ref={modalRef}>
                <button
                    className={`flex gap-3 items-center px-2.5 py-2 cursor-pointer transition ${open ? "text-slate-800 bg-slate-100" : "hover:bg-slate-100 text-slate-600 hover:text-slate-800"} w-full rounded-lg`}
                    onClick={() => {
                        setOpen(!open);
                    }}
                >
                    <RiCalendarScheduleLine className="w-4 h-4" />
                    {value ? <span>{format(value, 'dd MMM, yyyy')}</span> : "Not Set"}
                </button>
                <Calendar
                    date={value}
                    active={open}
                    close={() => setOpen(false)}
                    onSubmit={onChange}
                />
            </div>
        </div>
    )
}
