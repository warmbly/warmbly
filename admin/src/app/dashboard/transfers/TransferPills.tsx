// Status and kind pills for transfer jobs, shared by the instance-wide list
// and the per-workspace tab.

import { Loader2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import type { OrgTransferStatus } from "@/lib/api/client/admin/transfers";
import { cn } from "@/lib/utils";

const STATUS_TONE: Record<OrgTransferStatus, string> = {
    queued: "border-zinc-300 text-zinc-700",
    running: "border-sky-300 bg-sky-50 text-sky-700",
    completed: "border-emerald-300 bg-emerald-50 text-emerald-700",
    failed: "border-red-300 bg-red-50 text-red-700",
    expired: "border-zinc-300 bg-zinc-50 text-zinc-500",
};

export function StatusPill({
    status,
    progress,
    stage,
}: {
    status: OrgTransferStatus;
    progress?: number;
    stage?: string;
}) {
    const running = status === "running";
    return (
        <div className="flex flex-col gap-0.5">
            <Badge variant="outline" className={cn("text-[10px]", STATUS_TONE[status] ?? "border-zinc-300 text-zinc-600")}>
                {running && <Loader2 className="size-3 animate-spin" />}
                {status}
                {running && progress != null && <span className="tabular-nums">{progress}%</span>}
            </Badge>
            {running && stage && <span className="text-[10px] text-muted-foreground">{stage}</span>}
        </div>
    );
}

export function KindPill({ kind }: { kind: "export" | "import" }) {
    return (
        <Badge
            variant="outline"
            className={cn(
                "text-[10px]",
                kind === "export" ? "border-zinc-300 text-zinc-700" : "border-purple-300 bg-purple-50 text-purple-700",
            )}
        >
            {kind}
        </Badge>
    );
}
