import type React from "react";

export default function AddBoxFeatures({ children }: { children: React.ReactNode }) {
    return (
        <div className="grid gap-3">
            {children}
        </div>
    )
}
