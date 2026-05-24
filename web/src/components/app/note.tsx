"use client"

import React from "react";

export default function CopyNote({ children }: { children: React.ReactNode }) {
    const [status, setStatus] = React.useState<number>(0);

    const HandleCopy = async () => {
        try {
            await navigator.clipboard.writeText(children ? children.toString() : "")
            setStatus(1)
        } catch {
            setStatus(2)
        }
    }
    return <div className="relative bg-gray-100 text-gray-500 rounded-lg px-3 py-2 flex my-2 gap-5">
        <div className="grow break-all">
            {children}
        </div>
        <div className={`hover:brightness-95 shrink-0 text-blue-500 h-fit px-3 rounded-md py-0.5 cursor-pointer flex gap-2 items-center${status === 3 && " !text-red-500"}`} onClick={() => HandleCopy()}>
            {status === 0 ? <>
                Copy
            </> : status === 1 ? <>
                Copied
            </> : status === 2 && <>
                Failed
            </>}
        </div>
    </div>
}
