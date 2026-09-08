// Stat tile for the operations pages, the same shape as OverviewPage's.

import type { LucideIcon } from "lucide-react";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

export function StatCard({
    icon: Icon,
    label,
    value,
    sub,
    loading,
    tone = "neutral",
}: {
    icon: LucideIcon;
    label: string;
    value: React.ReactNode;
    sub?: React.ReactNode;
    loading?: boolean;
    tone?: "neutral" | "warn" | "danger";
}) {
    return (
        <Card
            className={cn(
                tone === "warn" && "border-amber-300 bg-amber-50/40",
                tone === "danger" && "border-red-300 bg-red-50/40",
            )}
        >
            <CardContent className="p-4">
                <div className="flex items-center gap-2 text-xs font-medium uppercase tracking-wider text-muted-foreground">
                    <Icon className="size-3.5" />
                    {label}
                </div>
                <div className="mt-2 text-2xl font-semibold tabular-nums">
                    {loading ? <Skeleton className="h-7 w-16" /> : value}
                </div>
                {sub && <div className="mt-1 text-xs text-muted-foreground">{sub}</div>}
            </CardContent>
        </Card>
    );
}
