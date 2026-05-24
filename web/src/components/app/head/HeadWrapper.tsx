import type React from "react";

export default function HeadWrapper({
    children,
}: {
    children: React.ReactNode,
}) {
    return (
        <div className="flex items-center justify-between w-full gap-3 px-5 py-2.5 border-b border-zinc-200">
            {children}
        </div>
    )
}
