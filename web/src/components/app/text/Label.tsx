import React from "react"

export default function Label({
    htmlFor,
    children
}: {
    htmlFor: string,
    children: React.ReactNode
}) {
    return (
        <label htmlFor={htmlFor} className="block mb-2 text-md font-sans font-bold text-foreground">
            {children}
        </label>
    )
}
