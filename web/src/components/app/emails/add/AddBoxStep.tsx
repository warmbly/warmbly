import React from "react"

export default function AddBoxStep({ step, children }: { step: number, children: React.ReactNode }) {
    return <div className="flex gap-7">
        <div className="shrink-0 bg-indigo-100 text-indigo-700 px-4 py-2 rounded-full h-fit">
            {step}
        </div>
        <div className="text-gray-600 grow text-lg mt-0.5">{children}</div>
    </div>
}
