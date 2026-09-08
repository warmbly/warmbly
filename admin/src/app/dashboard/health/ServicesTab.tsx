// Setup and health, services: live probes against the platform's backing
// services. The backend runs them on each request, so every fetch is a real
// round-trip to postgres, redis, the event bus and friends, not a cached view.

import { useQuery } from "@tanstack/react-query";
import { CheckCircle2, RefreshCw, XCircle } from "lucide-react";
import { ErrorState } from "@/components/ErrorState";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { MailStatusCard } from "../MailStatusCard";
import { getSystemStatus, type SystemComponentStatus } from "@/lib/api/client/admin/system";

export const SYSTEM_STATUS_KEY = ["admin", "system", "status"] as const;

// What breaks when each known component is down. Unknown names fall back
// to a generic line so new probes render without a frontend change.
const COMPONENT_BLURBS: Record<string, string> = {
    postgres: "Primary datastore. Everything depends on it.",
    redis: "Caching, rate limits, and the realtime event bridge in dev.",
    kafka: "Worker command and result transport. Nothing sends without it.",
    nats: "Worker command and result transport. Nothing sends without it.",
    "schema-registry": "Tracking-event encoding. Open and click events cannot serialize without it.",
    realtime: "Live dashboard updates (websockets).",
    tracking: "Open and click tracking ingestion.",
};

const GENERIC_BLURB = "Backing service probed by the backend health check.";

// "schema-registry" -> "Schema registry".
function titleCase(name: string): string {
    const spaced = name.replace(/[-_]+/g, " ").trim();
    if (!spaced) return name;
    return spaced.charAt(0).toUpperCase() + spaced.slice(1);
}

export function ServicesTab() {
    // Probes have no realtime event, so this is a deliberate poll; the query
    // only lives while the tab is mounted.
    const statusQ = useQuery({
        queryKey: SYSTEM_STATUS_KEY,
        queryFn: getSystemStatus,
        refetchInterval: 15_000,
        retry: false,
    });

    const components = statusQ.data?.data ?? [];
    const failing = components.filter((c) => !c.ok);

    return (
        <div>
            <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
                <p className="max-w-2xl text-sm text-muted-foreground">
                    Live health probes against the platform&apos;s backing services and its own
                    mail transport, run by the backend on each refresh.
                </p>
                <div className="flex items-center gap-2">
                    {statusQ.data && (
                        <span className="text-xs text-muted-foreground">
                            Last checked {new Date(statusQ.data.checked_at).toLocaleTimeString()}
                        </span>
                    )}
                    <Button
                        size="sm"
                        variant="outline"
                        onClick={() => statusQ.refetch()}
                        disabled={statusQ.isFetching}
                    >
                        <RefreshCw className={`size-4 ${statusQ.isFetching ? "animate-spin" : ""}`} />
                        {statusQ.isFetching ? "Checking..." : "Run checks"}
                    </Button>
                </div>
            </div>

            {statusQ.isLoading && (
                <div className="space-y-3">
                    <Skeleton className="h-12 w-full" />
                    <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
                        <Skeleton className="h-32 w-full" />
                        <Skeleton className="h-32 w-full" />
                        <Skeleton className="h-32 w-full" />
                        <Skeleton className="h-32 w-full" />
                    </div>
                </div>
            )}

            {statusQ.isError && (
                <ErrorState
                    error={statusQ.error}
                    title="Could not run health checks"
                    onRetry={() => statusQ.refetch()}
                />
            )}

            {statusQ.data && (
                <>
                    {failing.length === 0 ? (
                        <div className="mb-4 flex items-center gap-2 rounded-md border border-emerald-200 bg-emerald-50 p-3 text-sm text-emerald-800">
                            <CheckCircle2 className="size-4 shrink-0" />
                            <span className="font-medium">All systems operational</span>
                        </div>
                    ) : (
                        <div className="mb-4 flex items-center gap-2 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-800">
                            <XCircle className="size-4 shrink-0" />
                            <span>
                                <span className="font-medium">
                                    {failing.length === 1
                                        ? "1 component is down: "
                                        : `${failing.length} components are down: `}
                                </span>
                                {failing.map((c) => titleCase(c.name)).join(", ")}
                            </span>
                        </div>
                    )}

                    {components.length === 0 && (
                        <div className="mb-4 text-sm text-muted-foreground">
                            The status endpoint returned no components.
                        </div>
                    )}

                    <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
                        <MailStatusCard />
                        {components.map((c) => (
                            <ComponentCard key={c.name} component={c} />
                        ))}
                    </div>
                </>
            )}
        </div>
    );
}

function ComponentCard({ component: c }: { component: SystemComponentStatus }) {
    return (
        <Card className={c.ok ? "" : "border-red-200 bg-red-50/60"}>
            <CardHeader>
                <CardTitle className="flex items-center justify-between gap-2">
                    <span className="truncate">{titleCase(c.name)}</span>
                    <Badge
                        variant="outline"
                        className={
                            c.ok
                                ? "border-emerald-300 bg-emerald-50 text-emerald-700"
                                : "border-red-300 bg-red-50 text-red-700"
                        }
                    >
                        {c.ok ? "Operational" : "Down"}
                    </Badge>
                </CardTitle>
            </CardHeader>
            <CardContent className="space-y-2 pt-0">
                <p className="text-xs text-muted-foreground">
                    {COMPONENT_BLURBS[c.name] ?? GENERIC_BLURB}
                </p>
                <div className="text-xs text-muted-foreground">
                    Latency:{" "}
                    <span className="tabular-nums text-foreground">{c.latency_ms} ms</span>
                </div>
                {c.error && (
                    <div className="break-words rounded-md bg-red-100/60 p-2 font-mono text-[11px] text-red-800">
                        {c.error}
                    </div>
                )}
            </CardContent>
        </Card>
    );
}
