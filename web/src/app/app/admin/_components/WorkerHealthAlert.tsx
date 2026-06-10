// Cross-page worker health banner. Renders at the top of every admin page
// when any worker needs attention (errored, offline, or stuck mid-install).
// Click the banner to jump into the workers list with the relevant filter
// pre-applied.

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useLocation } from "react-router-dom";
import { listManagedWorkers } from "@/lib/api/client/app/admin/workers";

const OFFLINE_MS = 5 * 60_000;

export default function WorkerHealthAlert() {
    const { pathname } = useLocation();
    // Don't render on the workers list page itself — that page has its own
    // richer banner with filter chips, no point doubling up.
    const onWorkersPage = pathname.startsWith("/app/admin/workers");

    const { data } = useQuery({
        queryKey: ["admin", "workers", "managed"],
        queryFn: listManagedWorkers,
        refetchInterval: 30_000,
        // Stale time covers fast nav between admin pages without re-fetching.
        staleTime: 10_000,
    });

    const summary = useMemo(() => {
        let errored = 0, offline = 0, inProgress = 0;
        for (const w of data?.data ?? []) {
            if (w.install_state === "error") {
                errored++;
                continue;
            }
            if (
                w.install_state === "pending" ||
                w.install_state === "provisioning" ||
                w.install_state === "uninstalling"
            ) {
                inProgress++;
                continue;
            }
            if (w.install_state !== "installed") continue;
            const seenAge = w.last_seen_at ? Date.now() - new Date(w.last_seen_at).getTime() : Infinity;
            if (seenAge > OFFLINE_MS) offline++;
        }
        return { errored, offline, inProgress, total: errored + offline + inProgress };
    }, [data]);

    if (onWorkersPage || summary.total === 0) return null;

    // Pick the most urgent category first.
    const tone = summary.errored > 0 || summary.offline > 0 ? "red" : "amber";
    const toneCls = tone === "red"
        ? "bg-red-50 border-red-300 text-red-800"
        : "bg-amber-50 border-amber-300 text-amber-800";

    const parts: string[] = [];
    if (summary.errored > 0) parts.push(`${summary.errored} errored`);
    if (summary.offline > 0) parts.push(`${summary.offline} offline`);
    if (summary.inProgress > 0) parts.push(`${summary.inProgress} in progress`);

    return (
        <div className={`border rounded-lg px-3 py-2 mb-4 text-sm flex flex-wrap items-center justify-between gap-x-3 gap-y-1 ${toneCls}`}>
            <div>
                <span className="font-semibold">
                    {summary.total} worker{summary.total === 1 ? "" : "s"} need attention
                </span>
                <span className="ml-2 opacity-80">— {parts.join(" · ")}</span>
            </div>
            <Link
                to="/app/admin/workers"
                className="shrink-0 text-xs font-medium underline hover:no-underline"
            >
                review →
            </Link>
        </div>
    );
}
