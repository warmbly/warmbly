import type React from "react";

export default function HeadButton({
    children,
    onClick,
    white,
}: {
    children: React.ReactNode,
    onClick: () => Promise<void> | void,
    white?: boolean,
}) {
    return (
        <button onClick={onClick} className={`px-3 py-1.5 cursor-pointer rounded-md text-sm transition-colors duration-150 ease-in-out flex items-center gap-1.5 select-none ${white ? "text-muted-foreground hover:text-foreground" : "bg-zinc-900 text-white hover:bg-zinc-800"}`}>
            {children}
        </button>
    )
}
