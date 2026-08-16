// Setup and health: everything this deployment is currently getting wrong,
// as decided by the running backend. The endpoint returns only checks that
// are not ok, so an empty response is a real all-clear and not a stub.

import { Link } from "react-router-dom";
import { AlertTriangle, CheckCircle2, Info, RefreshCw, XCircle } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { ErrorState } from "@/components/ErrorState";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { InstanceFindings } from "./InstanceHealthPanel";
import { useInstanceHealth } from "@/hooks/useInstanceHealth";
import type { InstanceHealthSummary } from "@/lib/api/client/admin/instance";

export default function HealthPage() {
    const healthQ = useInstanceHealth();

    const checks = healthQ.data?.checks ?? [];
    const summary = healthQ.data?.summary;

    return (
        <div>
            <PageHeader
                title="Setup and health"
                description="Checks the backend runs against this instance: secrets, addresses, platform mail, accounts, workers and storage. Only findings that need a decision are listed."
            >
                {healthQ.dataUpdatedAt > 0 && (
                    <span className="text-xs text-muted-foreground">
                        Last checked {new Date(healthQ.dataUpdatedAt).toLocaleTimeString()}
                    </span>
                )}
                <Button size="sm" variant="outline" asChild>
                    <Link to="/configuration">Configuration</Link>
                </Button>
                <Button
                    size="sm"
                    variant="outline"
                    onClick={() => healthQ.refetch()}
                    disabled={healthQ.isFetching}
                >
                    <RefreshCw className={`size-4 ${healthQ.isFetching ? "animate-spin" : ""}`} />
                    {healthQ.isFetching ? "Checking..." : "Run checks"}
                </Button>
            </PageHeader>

            {healthQ.isLoading && (
                <div className="space-y-3">
                    <Skeleton className="h-12 w-full" />
                    <Skeleton className="h-20 w-full" />
                    <Skeleton className="h-20 w-full" />
                </div>
            )}

            {healthQ.isError && (
                <ErrorState
                    error={healthQ.error}
                    title="Could not run the instance checks"
                    onRetry={() => healthQ.refetch()}
                />
            )}

            {healthQ.data && (
                <>
                    {checks.length === 0 ? (
                        <div className="rounded-lg border border-emerald-200 bg-emerald-50 p-4">
                            <div className="flex items-start gap-3">
                                <CheckCircle2 className="mt-0.5 size-4 shrink-0 text-emerald-600" />
                                <div>
                                    <div className="text-sm font-semibold text-emerald-800">
                                        No problems found
                                    </div>
                                    <p className="mt-0.5 text-[13px] leading-relaxed text-emerald-700">
                                        Every setup and health check passed. This page lists only the
                                        checks that need attention, so it stays empty while the
                                        instance is configured correctly.
                                    </p>
                                </div>
                            </div>
                        </div>
                    ) : (
                        <>
                            <SummaryStrip summary={summary} total={checks.length} />
                            <InstanceFindings checks={checks} />
                        </>
                    )}
                </>
            )}
        </div>
    );
}

function SummaryStrip({
    summary,
    total,
}: {
    summary: InstanceHealthSummary | undefined;
    total: number;
}) {
    const errors = summary?.error ?? 0;
    const warnings = summary?.warning ?? 0;
    const info = summary?.info ?? 0;

    return (
        <div className="mb-5 grid grid-cols-1 gap-3 sm:grid-cols-3">
            <SummaryCard
                icon={XCircle}
                label="Errors"
                value={errors}
                sub="Something is broken right now"
                tone={errors > 0 ? "border-red-200 bg-red-50/60" : ""}
                iconClass={errors > 0 ? "text-red-600" : "text-muted-foreground"}
            />
            <SummaryCard
                icon={AlertTriangle}
                label="Warnings"
                value={warnings}
                sub="Works, but not the way you want"
                tone={warnings > 0 ? "border-amber-200 bg-amber-50/60" : ""}
                iconClass={warnings > 0 ? "text-amber-600" : "text-muted-foreground"}
            />
            <SummaryCard
                icon={Info}
                label="Worth knowing"
                value={info}
                sub={`${total} findings in total`}
                tone={info > 0 ? "border-sky-200 bg-sky-50/50" : ""}
                iconClass={info > 0 ? "text-sky-600" : "text-muted-foreground"}
            />
        </div>
    );
}

function SummaryCard({
    icon: Icon,
    label,
    value,
    sub,
    tone,
    iconClass,
}: {
    icon: React.ComponentType<{ className?: string }>;
    label: string;
    value: number;
    sub: string;
    tone: string;
    iconClass: string;
}) {
    return (
        <div className={`rounded-lg border border-border bg-white p-3 ${tone}`}>
            <div className="flex items-center gap-2 text-xs font-medium uppercase tracking-wider text-muted-foreground">
                <Icon className={`size-3.5 ${iconClass}`} />
                {label}
            </div>
            <div className="mt-1.5 text-2xl font-semibold tabular-nums">{value}</div>
            <div className="mt-0.5 text-xs text-muted-foreground">{sub}</div>
        </div>
    );
}
