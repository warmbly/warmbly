import type React from "react";

export default function AddBoxTop({ children }: { children: React.ReactNode }) {
    return (
        <div className="flex gap-5 items-center mb-8">
            {children}
        </div>
    )
}
