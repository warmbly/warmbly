// Effective limits: what this instance is actually enforcing right now,
// after configuration, plan and product defaults have all been applied.
// Read only, because every number here is owned by one of those layers.

import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { SlidersHorizontal } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { ErrorState } from "@/components/ErrorState";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
    getInstanceLimits,
    type InstanceLimitGroup,
} from "@/lib/api/client/admin/instance";

export default function LimitsPage() {
    const limitsQ = useQuery({
        queryKey: ["admin", "instance", "limits"],
        queryFn: getInstanceLimits,
        retry: false,
    });

    const groups = limitsQ.data?.groups ?? [];

    return (
        <div>
            <PageHeader
                title="Effective limits"
                description="The caps and defaults this instance enforces, resolved from configuration and the product defaults. Read-only: change the matching variable or the organization's override instead."
            >
                <Button size="sm" variant="outline" asChild>
                    <Link to="/configuration">
                        <SlidersHorizontal className="size-4" />
                        Configuration
                    </Link>
                </Button>
            </PageHeader>

            {limitsQ.isLoading && (
                <div className="space-y-3">
                    <Skeleton className="h-40 w-full" />
                    <Skeleton className="h-40 w-full" />
                </div>
            )}

            {limitsQ.isError && (
                <ErrorState
                    error={limitsQ.error}
                    title="Could not load the effective limits"
                    onRetry={() => limitsQ.refetch()}
                />
            )}

            {limitsQ.data && groups.length === 0 && (
                <div className="rounded-lg border border-dashed border-border p-10 text-center text-sm text-muted-foreground">
                    The backend returned no limits.
                </div>
            )}

            <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
                {groups.map((group) => (
                    <LimitGroupCard key={group.title} group={group} />
                ))}
            </div>
        </div>
    );
}

function LimitGroupCard({ group }: { group: InstanceLimitGroup }) {
    const entries = group.entries ?? [];
    return (
        <section className="overflow-hidden rounded-lg border border-border bg-white">
            <div className="border-b border-border px-4 py-2.5 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
                {group.title}
            </div>
            <div className="divide-y divide-border">
                {entries.length === 0 && (
                    <div className="px-4 py-3 text-xs text-muted-foreground">
                        No limits in this group.
                    </div>
                )}
                {entries.map((entry) => (
                    <div
                        key={entry.name}
                        className="flex items-start justify-between gap-4 px-4 py-3"
                    >
                        <div className="min-w-0">
                            <div className="text-sm font-medium text-foreground">{entry.name}</div>
                            {entry.description && (
                                <p className="mt-0.5 text-xs leading-relaxed text-muted-foreground">
                                    {entry.description}
                                </p>
                            )}
                        </div>
                        <div className="shrink-0 text-right">
                            <div className="text-sm font-semibold tabular-nums text-foreground">
                                {entry.value}
                            </div>
                            {entry.unit && (
                                <div className="text-[11px] text-muted-foreground">{entry.unit}</div>
                            )}
                        </div>
                    </div>
                ))}
            </div>
        </section>
    );
}
