
import { RiCheckLine } from "@remixicon/react";
import type React from "react";

export default function AddBoxFeature({ children }: { children: React.ReactNode }) {
    return (
        <div className="flex gap-5">
            <div className="bg-indigo-100 text-indigo-600 px-3 h-fit rounded-full">
                <RiCheckLine className="w-4" />
            </div>
            <p className="text-gray-600">{children}</p>
        </div>

    )
}
