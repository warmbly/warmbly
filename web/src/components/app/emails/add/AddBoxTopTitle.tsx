import type React from "react";

export default function AddBoxTopTitle({ children }: { children: React.ReactNode }) {
    return (
        <h1 className="font-semibold text-xl">
            {children}
        </h1>
    )
}
