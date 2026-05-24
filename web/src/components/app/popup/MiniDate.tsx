import React from "react";
import Calendar from "../Calendar";
import { format } from "date-fns";

export default function MiniDate({
    onChange,
    placeholder,
    value
}: {
    onChange: (v: Date | undefined) => void,
    placeholder: string,
    value: Date | undefined
}) {
    const [drop, setDrop] = React.useState<boolean>(false);
    const modalRef = React.useRef<HTMLDivElement>(null);

    React.useEffect(() => {
        const handleClickOutside = (event: MouseEvent) => {
            if (modalRef.current && !modalRef.current.contains(event.target as Node)) {
                setDrop(false);
            }
        };
        if (drop) {
            document.addEventListener('mousedown', handleClickOutside);
        }

        return () => {
            document.removeEventListener('mousedown', handleClickOutside);
        };
    }, [drop]);

    return (<div className="relative w-full" ref={modalRef}>
        <div onClick={() => setDrop(true)} className={`w-full cursor-pointer font-sans text-[15px] bg-transparent text-slate-700 border rounded-md px-3 py-2.5 transition duration-300 ease focus:outline-none ${drop ? "border-slate-300" : "border-slate-200 hover:border-slate-300"} shadow-sm focus:shadow`}>
            {value ? <span>{format(value, 'dd MMM, yyyy')}</span> : <span className="text-slate-400">{placeholder}</span>}
        </div>
        <Calendar
            date={value || null}
            active={drop}
            close={() => setDrop(false)}
            onSubmit={(v) => onChange(v || undefined)}
        />
    </div>)
}
