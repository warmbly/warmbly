// Tab bar for pages that split into sections (Setup and health,
// Configuration, ...). Controlled: the page owns the value and usually
// mirrors it into `?tab=` so links deep-link. Same shape as the dashboard's
// drawer tabs: icon + label, amber underline on the active one.

import type { KeyboardEvent } from "react";
import type { LucideIcon } from "lucide-react";
import { cn } from "@/lib/utils";

export interface PageTab {
    id: string;
    label: string;
    icon: LucideIcon;
    badge?: number;
}

interface Props {
    tabs: PageTab[];
    value: string;
    onChange: (id: string) => void;
    className?: string;
}

export function PageTabs({ tabs, value, onChange, className }: Props) {
    function onKeyDown(e: KeyboardEvent<HTMLDivElement>) {
        if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
        const idx = tabs.findIndex((t) => t.id === value);
        if (idx < 0) return;
        e.preventDefault();
        const step = e.key === "ArrowRight" ? 1 : -1;
        const next = tabs[(idx + step + tabs.length) % tabs.length];
        onChange(next.id);
        const el = e.currentTarget.querySelector<HTMLButtonElement>(`[data-tab="${next.id}"]`);
        el?.focus();
    }

    return (
        <div
            role="tablist"
            onKeyDown={onKeyDown}
            className={cn(
                "shrink-0 flex items-center gap-1 border-b border-border overflow-x-auto no-scrollbar mb-5",
                className,
            )}
        >
            {tabs.map(({ id, label, icon: Icon, badge }) => {
                const active = id === value;
                return (
                    <button
                        key={id}
                        type="button"
                        role="tab"
                        data-tab={id}
                        aria-selected={active}
                        tabIndex={active ? 0 : -1}
                        onClick={() => onChange(id)}
                        className={cn(
                            "relative h-10 px-2.5 inline-flex items-center gap-1.5 text-[12.5px] whitespace-nowrap transition-colors",
                            active
                                ? "text-foreground font-medium"
                                : "text-muted-foreground hover:text-foreground",
                        )}
                    >
                        <Icon className="size-3.5 shrink-0" />
                        {label}
                        {badge !== undefined && badge > 0 && (
                            <span
                                className={cn(
                                    "rounded-full px-1.5 text-[10px] font-semibold leading-4 tabular-nums",
                                    active
                                        ? "bg-[var(--admin-accent-soft)] text-[var(--admin-accent-strong)]"
                                        : "bg-muted text-muted-foreground",
                                )}
                            >
                                {badge}
                            </span>
                        )}
                        {active && (
                            <span className="absolute inset-x-0 -bottom-px h-0.5 rounded-t bg-[var(--admin-accent)]" />
                        )}
                    </button>
                );
            })}
        </div>
    );
}
