import { RiSearchLine } from "@remixicon/react";

export default function Search({value, onChange}:{value: string, onChange: (v: string) => void}){
    return (
        <div className="bg-white w-full h-full sm:max-w-sm flex px-3 gap-1 items-center border border-border rounded-md">
            <RiSearchLine className="w-4 h-4 shrink-0 text-muted-foreground"/>
            <input
            className="text-sm text-foreground grow font-sans outline-none py-1.5"
            placeholder="Search..."
            value={value} onChange={(e) => onChange(e.target.value)}
            />
        </div>
    )
}