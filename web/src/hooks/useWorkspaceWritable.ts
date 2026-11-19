import usePartnerManaged from "@/hooks/usePartnerManaged";
import { queryClient } from "@/lib/queryClient";
import { currentOrganizationQuery } from "@/lib/api/hooks/app/organizations/useCurrentOrganization";
import type { PartnerManaged } from "@/lib/api/models/app/organizations/Organization";
import { partnerPaused, partnerPauseMessage } from "@/lib/partnerPause";

export interface WorkspaceWritable {
    /** False while the reseller has the workspace suspended or cancelled: every write is refused. */
    writable: boolean;
    /** Why not, for a tooltip or a dialog; undefined while writable. */
    reason: string | undefined;
}

const WRITABLE: WorkspaceWritable = { writable: true, reason: undefined };

export function workspaceWritable(partner: PartnerManaged | null | undefined): WorkspaceWritable {
    return partnerPaused(partner) ? { writable: false, reason: partnerPauseMessage(partner) } : WRITABLE;
}

/** Whether the current workspace takes writes. The create, launch and save controls consult it. */
export default function useWorkspaceWritable(): WorkspaceWritable {
    return workspaceWritable(usePartnerManaged());
}

/** The non-hook read, for keyboard shortcuts and other event handlers. */
export function checkWorkspaceWritable(): WorkspaceWritable {
    return workspaceWritable(queryClient.getQueryData(currentOrganizationQuery.queryKey)?.partner);
}

/** Explain a held write in the app's permission dialog, the same one a refused request opens. */
export function showWorkspaceReadOnly(reason: string | undefined) {
    if (typeof window === "undefined") return;
    window.dispatchEvent(new CustomEvent("permission-denied", { detail: { code: "organization_suspended", message: reason } }));
}
