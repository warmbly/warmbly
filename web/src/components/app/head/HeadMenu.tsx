import type React from "react";

export default function HeadMenu({
    children,
}: {
    children: React.ReactNode,
}) {
    return (
        <div className="flex items-center gap-2">
            {children}
        </div>
    )
}
