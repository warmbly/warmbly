import type React from "react";

export default function AddBox({ children }: { children: React.ReactNode }) {
    return (
        <div className="rounded-lg p-5 flex flex-col justify-between shadow-lg bg-gray-50">
            {children}
        </div>
    )
}
