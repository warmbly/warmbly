// Shared component for the Settings → {Encryption,Storage,Messaging,
// Cache,Transports} pages. They all hit the same backend registry
// (/admin/settings/backends) with a different `kind` filter, so we
// keep one component and pass kind+title+description in from the
// route-level wrapper.

import { useQuery } from "@tanstack/react-query";
import { CheckCircle2, Settings2 } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import {
    getActiveBackend,
    listStorageBackends,
} from "@/lib/api/client/admin/settings";
import type { StorageBackend, StorageBackendKind } from "@/lib/api/models/admin";

interface BackendsPageProps {
    kind: StorageBackendKind;
    title: string;
    description: string;
    /** Plain-English copy of the registry's role for the surface. */
    notes?: string;
}

export function BackendsPage({ kind, title, description, notes }: BackendsPageProps) {
    const listQ = useQuery({
        queryKey: ["admin", "settings", "backends", kind],
        queryFn: () => listStorageBackends(kind),
        retry: false,
    });
    const activeQ = useQuery({
        queryKey: ["admin", "settings", "backends", "active", kind],
        queryFn: () => getActiveBackend(kind),
        retry: false,
    });

    const backends = listQ.data ?? [];
    const active = activeQ.data;
    const isPending = listQ.isLoading || activeQ.isLoading;

    return (
        <div>
            <PageHeader title={title} description={description} />

            {/* Active provider card. Lives at the top so the most-asked
                question ("what are we actually using?") is the headline. */}
            <Card className="mb-4">
                <CardHeader>
                    <CardTitle className="flex items-center gap-2">
                        <CheckCircle2 className="size-4 text-[var(--admin-accent)]" />
                        Active provider
                    </CardTitle>
                    <CardDescription>
                        The backend currently fielding {kind} requests for the platform.
                    </CardDescription>
                </CardHeader>
                <CardContent className="pt-0">
                    {isPending && <Skeleton className="h-12 w-1/2" />}
                    {!isPending && active && <ActiveCard backend={active} />}
                    {!isPending && !active && (
                        <Placeholder
                            title="Endpoint pending"
                            body={`The /admin/settings/backends/active/${kind} endpoint hasn't returned anything yet. Either no provider is configured for ${kind}, or this backend route is still being wired by the API team.`}
                        />
                    )}
                </CardContent>
            </Card>

            {/* Registered providers — the full list. */}
            <Card>
                <CardHeader>
                    <CardTitle className="flex items-center gap-2">
                        <Settings2 className="size-4" />
                        Registered providers
                    </CardTitle>
                    <CardDescription>
                        Everything registered under <code>kind={kind}</code>. Activating one swaps the
                        platform's live provider — that swap is logged to the audit feed.
                    </CardDescription>
                </CardHeader>
                <CardContent className="pt-0">
                    {isPending && (
                        <div className="space-y-2">
                            <Skeleton className="h-10 w-full" />
                            <Skeleton className="h-10 w-full" />
                        </div>
                    )}
                    {!isPending && backends.length === 0 && (
                        <Placeholder
                            title="No providers registered"
                            body={`When the API team wires the /admin/settings/backends registry for ${kind}, the registered providers will show up here. Until then, the platform falls back to its env-var-configured default.`}
                        />
                    )}
                    {!isPending && backends.length > 0 && (
                        <ul className="divide-y divide-border">
                            {backends.map((b) => (
                                <BackendRow key={b.id} backend={b} isActive={b.id === active?.id} />
                            ))}
                        </ul>
                    )}
                </CardContent>
            </Card>

            {notes && (
                <div className="mt-4 text-xs text-muted-foreground border-l-2 border-[var(--admin-accent)] pl-3">
                    {notes}
                </div>
            )}
        </div>
    );
}

function ActiveCard({ backend }: { backend: StorageBackend }) {
    return (
        <div className="rounded-md border border-emerald-200 bg-emerald-50/60 p-3">
            <div className="flex items-center gap-2 text-sm font-medium">
                <span>{backend.label || backend.provider}</span>
                <Badge variant="outline" className="text-[10px] border-emerald-300 text-emerald-700">
                    active
                </Badge>
            </div>
            <div className="text-xs text-muted-foreground mt-1 font-mono">
                {backend.provider} · {backend.id}
            </div>
            {backend.config && Object.keys(backend.config).length > 0 && (
                <details className="mt-2">
                    <summary className="text-[11px] text-muted-foreground cursor-pointer">
                        Configuration
                    </summary>
                    <pre className="mt-1 text-[11px] bg-background border border-border rounded p-2 overflow-auto font-mono">
                        {JSON.stringify(redactSecrets(backend.config), null, 2)}
                    </pre>
                </details>
            )}
        </div>
    );
}

function BackendRow({ backend, isActive }: { backend: StorageBackend; isActive: boolean }) {
    return (
        <li className="py-2.5 flex items-center gap-3">
            <span
                className={`size-2 rounded-full ${
                    isActive ? "bg-emerald-500" : "bg-zinc-300"
                }`}
            />
            <div className="min-w-0 flex-1">
                <div className="text-sm font-medium truncate">
                    {backend.label || backend.provider}
                </div>
                <div className="text-[10px] text-muted-foreground font-mono truncate">
                    {backend.provider} · {backend.id}
                </div>
            </div>
            {isActive ? (
                <Badge className="bg-emerald-600 text-[10px]">in use</Badge>
            ) : (
                <Badge variant="outline" className="text-[10px]">standby</Badge>
            )}
        </li>
    );
}

function Placeholder({ title, body }: { title: string; body: string }) {
    return (
        <div className="rounded-md border border-dashed border-border bg-muted/30 p-4">
            <div className="text-sm font-medium">{title}</div>
            <p className="text-xs text-muted-foreground mt-1 max-w-2xl">{body}</p>
        </div>
    );
}

// Best-effort secret redaction for display. The registry shouldn't be
// returning raw secrets to the UI in the first place, but if it does
// we at least don't paint them on-screen verbatim.
function redactSecrets(cfg: Record<string, unknown>): Record<string, unknown> {
    const out: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(cfg)) {
        if (/key|secret|password|token/i.test(k) && typeof v === "string") {
            out[k] = v.length > 8 ? `${v.slice(0, 4)}…${v.slice(-2)}` : "***";
        } else {
            out[k] = v;
        }
    }
    return out;
}
