// Status and folder badges for placement tests.

import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import type { PlacementFolder, PlacementStatus } from "@/lib/api/client/admin/placement";

const STATUS_TONE: Record<PlacementStatus, string> = {
    running: "border-amber-300 bg-amber-50 text-amber-700",
    completed: "border-emerald-300 bg-emerald-50 text-emerald-700",
    cancelled: "border-zinc-300 bg-zinc-50 text-zinc-600",
    failed: "border-red-300 bg-red-50 text-red-700",
};

export function StatusBadge({ status }: { status: string }) {
    return (
        <Badge
            variant="outline"
            className={cn("text-[10px]", STATUS_TONE[status as PlacementStatus] ?? "border-zinc-300 text-zinc-600")}
        >
            {status}
        </Badge>
    );
}

const FOLDER_LABEL: Record<PlacementFolder, string> = {
    pending: "Pending",
    inbox: "Primary inbox",
    promotions: "Promotions",
    other: "Other tab",
    spam: "Spam",
    missing: "Missing",
    failed: "Failed",
    cancelled: "Cancelled",
};

const FOLDER_TONE: Record<PlacementFolder, string> = {
    pending: "border-zinc-300 bg-zinc-50 text-zinc-600",
    inbox: "border-emerald-300 bg-emerald-50 text-emerald-700",
    promotions: "border-sky-300 bg-sky-50 text-sky-700",
    other: "border-sky-300 bg-sky-50 text-sky-700",
    spam: "border-red-300 bg-red-50 text-red-700",
    missing: "border-amber-300 bg-amber-50 text-amber-700",
    failed: "border-red-300 text-red-700",
    cancelled: "border-zinc-300 text-zinc-500",
};

export function FolderBadge({ folder }: { folder: string }) {
    const f = folder as PlacementFolder;
    return (
        <Badge variant="outline" className={cn("text-[10px]", FOLDER_TONE[f] ?? "border-zinc-300 text-zinc-600")}>
            {FOLDER_LABEL[f] ?? folder}
        </Badge>
    );
}
