// Status and folder badges for placement tests.

import { StatusBadge } from "@/components/ui/kit";
import type { Tone } from "@/lib/tones";
import type { PlacementFolder, PlacementStatus } from "@/lib/api/client/admin/placement";

const STATUS_TONE: Record<PlacementStatus, Tone> = {
    running: "info",
    completed: "success",
    cancelled: "neutral",
    failed: "danger",
};

export function TestStatusBadge({ status }: { status: string }) {
    return (
        <StatusBadge tone={STATUS_TONE[status as PlacementStatus] ?? "neutral"} dot>
            {status}
        </StatusBadge>
    );
}

const FOLDER_LABEL: Record<PlacementFolder, string> = {
    pending: "Pending",
    inbox: "Primary inbox",
    promotions: "Promotions",
    other: "Other tab",
    spam: "Spam",
    missing: "Missing",
    unknown: "Folder unknown",
    archive: "Archive",
    custom: "Custom folder",
    failed: "Failed",
    cancelled: "Cancelled",
};

const FOLDER_TONE: Record<PlacementFolder, Tone> = {
    pending: "neutral",
    inbox: "success",
    promotions: "info",
    other: "info",
    spam: "danger",
    missing: "warning",
    unknown: "neutral",
    archive: "neutral",
    custom: "neutral",
    failed: "danger",
    cancelled: "neutral",
};

export function FolderBadge({ folder }: { folder: string }) {
    const f = folder as PlacementFolder;
    return (
        <StatusBadge tone={FOLDER_TONE[f] ?? "neutral"} dot>
            {FOLDER_LABEL[f] ?? folder}
        </StatusBadge>
    );
}
