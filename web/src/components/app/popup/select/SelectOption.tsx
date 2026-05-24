import { RiCheckLine } from "@remixicon/react";
import React from "react"

export default function SelectOption({ children, color, selected, onClick }: { children: React.ReactNode, color?: string, selected?: boolean, onClick: () => void }) {
    return (
        <button
            className={`px-3 py-2 flex gap-3 justify-between rounded-md items-center w-full cursor-pointer ${selected ? "bg-slate-100" : "hover:bg-slate-100"} text-slate-600`} style={color ? { color: `${color}` } : undefined}
            onClick={onClick}
        >
            <div className="flex gap-3 overflow-hidden">
                {children}
            </div>
            {selected && (
                <div className="flex items-center">
                    <div className="bg-blue-200 text-blue-600 w-6 h-6 flex items-center justify-center rounded-lg">
                        <RiCheckLine className="w-4" />
                    </div>
                </div>
            )}
        </button>
    )
}
