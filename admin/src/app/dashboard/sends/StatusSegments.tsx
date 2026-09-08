// Segmented status filter with a count per option. Explorer's SegmentedFilter
// is a fixed three-column grid with no badges, so status rails that need
// counts (dead letters) use this one; same visual language.

import { cn } from "@/lib/utils";

export function StatusSegments<T extends string>({
    value,
    onChange,
    options,
}: {
    value: T;
    onChange: (v: T) => void;
    options: { value: T; label: string; count?: number }[];
}) {
    return (
        <div className="inline-flex flex-wrap gap-0.5 rounded-md border border-border bg-card p-0.5 text-[11px]">
            {options.map((o) => {
                const active = value === o.value;
                return (
                    <button
                        key={o.value}
                        type="button"
                        onClick={() => onChange(o.value)}
                        className={cn(
                            "inline-flex items-center gap-1.5 rounded px-2 py-1 transition-colors",
                            active
                                ? "bg-[var(--admin-accent)] font-medium text-white"
                                : "text-muted-foreground hover:bg-muted/60 hover:text-foreground",
                        )}
                    >
                        {o.label}
                        {o.count !== undefined && (
                            <span
                                className={cn(
                                    "rounded-full px-1.5 text-[10px] font-semibold leading-4 tabular-nums",
                                    active ? "bg-white/25 text-white" : "bg-muted text-muted-foreground",
                                )}
                            >
                                {o.count.toLocaleString()}
                            </span>
                        )}
                    </button>
                );
            })}
        </div>
    );
}
