// What a member reads about a workspace its reseller paused: the app banner, the tooltip on a held
// control, and the dialog for a write the API refused. One wording for all three.
import type { PartnerManaged } from "@/lib/api/models/app/organizations/Organization";

/** The API's refusal of a write in a suspended or cancelled managed workspace. */
export const PARTNER_PAUSED_CODES: ReadonlySet<string> = new Set(["organization_suspended", "organization_cancelled"]);

export function isPartnerPausedCode(code: string | undefined): boolean {
    return !!code && PARTNER_PAUSED_CODES.has(code);
}

/** Whether the reseller paused the workspace: everything reads, nothing writes, and sending and warmup stop. */
export function partnerPaused(p: PartnerManaged | null | undefined): p is PartnerManaged {
    return !!p && p.state !== "active";
}

export function partnerPauseTitle(p: PartnerManaged): string {
    return p.suspended_reason === "billing" && p.state !== "cancelled" ? `Paused: billing through ${p.display_name}.` : `Paused by ${p.display_name}.`;
}

export function partnerPauseDetail(p: PartnerManaged): string {
    const deletes = p.state === "cancelled" && p.deletes_at ? new Date(p.deletes_at) : null;
    return (
        "Sending and warmup are stopped and this workspace is read-only. " +
        (deletes
            ? `It is deleted on ${deletes.toLocaleDateString()} unless ${p.display_name} restores it.`
            : `It resumes as soon as ${p.display_name} reactivates it.`)
    );
}

export function partnerPauseMessage(p: PartnerManaged): string {
    return `${partnerPauseTitle(p)} ${partnerPauseDetail(p)}`;
}

/** The partner as a refused write shows it: the API's code is newer than a cached "active". */
export function pausedFromRefusal(p: PartnerManaged, code: string): PartnerManaged {
    if (p.state !== "active") return p;
    return { ...p, state: code === "organization_cancelled" ? "cancelled" : "suspended" };
}
