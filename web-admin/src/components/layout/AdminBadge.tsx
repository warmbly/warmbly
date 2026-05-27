import { ShieldAlert } from "lucide-react";
import { cn } from "@/lib/utils";

// The persistent "ADMIN" badge. Rendered in the sidebar header so an
// admin who switches tabs from the dashboard immediately sees an
// elevated-privilege marker. The colour ties back to --admin-accent
// so every admin-only surface uses the same visual language.

interface AdminBadgeProps {
    className?: string;
    compact?: boolean;
}

export function AdminBadge({ className, compact = false }: AdminBadgeProps) {
    return (
        <span
            className={cn(
                "inline-flex items-center gap-1.5 rounded-md font-semibold uppercase tracking-wider",
                "bg-[var(--admin-accent)] text-[var(--admin-accent-foreground)]",
                "shadow-[0_1px_0_rgba(0,0,0,0.08)]",
                compact ? "px-1.5 py-0.5 text-[10px]" : "px-2 py-1 text-xs",
                className,
            )}
        >
            <ShieldAlert className={compact ? "size-3" : "size-3.5"} />
            Admin
        </span>
    );
}
