// Cursor — one teammate's live pointer, positioned in screen space by the layer
// that renders it (canvas or page). The label carries the person's real avatar
// (initials fallback) next to their name so it is always clear who it is. A
// short CSS transition smooths the gaps between throttled frames.

import React from "react";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";

function initialsOf(name: string | null): string {
    if (!name) return "?";
    const parts = name.trim().split(/\s+/).filter(Boolean);
    if (parts.length === 0) return "?";
    if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
    return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase();
}

export default function Cursor({
    color,
    name,
    avatar,
    left,
    top,
}: {
    color: string;
    name: string | null;
    avatar?: string | null;
    left: number;
    top: number;
}) {
    return (
        <div
            className="pointer-events-none absolute left-0 top-0 transition-transform duration-75 ease-linear will-change-transform"
            style={{ transform: `translate(${left}px, ${top}px)` }}
        >
            <svg
                width="18"
                height="18"
                viewBox="0 0 18 18"
                fill="none"
                style={{ filter: "drop-shadow(0 1px 1.5px rgba(15,23,42,0.25))" }}
            >
                <path
                    d="M2 2 L2 14 L5.6 10.6 L8 16 L10.6 14.9 L8.2 9.6 L13 9.6 Z"
                    fill={color}
                    stroke="white"
                    strokeWidth="1.1"
                    strokeLinejoin="round"
                />
            </svg>
            {name || avatar ? (
                <div
                    className="absolute left-3 top-3.5 flex max-w-[160px] items-center gap-1 rounded-full py-0.5 pl-0.5 pr-2 shadow-sm"
                    style={{ backgroundColor: color }}
                >
                    <Avatar className="size-3.5 shrink-0 ring-1 ring-white/80">
                        {avatar ? <AvatarImage src={avatar} alt={name ?? ""} /> : null}
                        <AvatarFallback className="bg-white/90 text-[7px] font-semibold text-slate-700">
                            {initialsOf(name)}
                        </AvatarFallback>
                    </Avatar>
                    {name ? (
                        <span className="truncate text-[10.5px] font-medium leading-none text-white">{name}</span>
                    ) : null}
                </div>
            ) : null}
        </div>
    );
}
