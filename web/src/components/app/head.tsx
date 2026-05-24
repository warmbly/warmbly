"use client";

import Link from "next/link";


export function Head({children}: {children: React.ReactNode}) {
    return <div className="flex gap-5 border-b border-gray-300">
        {children}
    </div>
}
export function HeadLink({children, href}: {children: React.ReactNode, href: string}){
    return <Link href={href}>
        {children}
    </Link>
}