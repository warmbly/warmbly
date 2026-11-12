// Address verification: who checks this workspace's contacts, and the member
// actions on a selection (re-verify, mark deliverable / undeliverable).

import Request from "../../Request";
import type ContactSelection from "@/lib/api/models/app/contacts/ContactSelection";
import type { ContactVerificationCounts } from "@/lib/api/models/app/contacts/SearchContactsResult";

export interface VerificationOverview {
    // "builtin" (the in-house check) or the connected verification provider.
    provider: "builtin" | "millionverifier" | "cleanmylist" | "mailveri" | string;
    connection_id?: string;
    credits?: number;
    // Set when a provider is connected but unusable (bad key, no credits).
    provider_error?: string;
    // Whether the built-in check can reach mail servers from this instance.
    builtin_ready: boolean;
    counts: ContactVerificationCounts;
}

export type VerificationAction = "verify" | "mark_deliverable" | "mark_undeliverable";

export interface VerificationRequest extends Partial<ContactSelection> {
    // Every lead of this campaign that verification refused.
    campaign_id?: string;
    action: VerificationAction;
}

export interface VerificationResponse {
    affected: number;
    action: VerificationAction;
    queued: boolean;
    // Who runs a queued check: "builtin" or the connected provider, with its
    // display name. verifier_error says why a connected provider is passed
    // over right now, in which case the built-in check runs instead.
    verifier?: string;
    verifier_label?: string;
    verifier_error?: string;
}

// The toast after queueing a re-check: who runs it, and a warning when the
// connected verifier cannot be used.
export function reverifyNotice(res: VerificationResponse, noun: string, nouns: string): { text: string; warn: boolean } {
    const what = `${res.affected.toLocaleString()} ${res.affected === 1 ? noun : nouns}`;
    if (res.verifier_error) return { text: `Queued ${what}. ${res.verifier_error}`, warn: true };
    if (res.verifier && res.verifier !== "builtin" && res.verifier_label) {
        return { text: `Re-verifying ${what} with ${res.verifier_label}`, warn: false };
    }
    return { text: `Re-checking ${what}`, warn: false };
}

export async function getContactVerification(): Promise<VerificationOverview> {
    return await Request<VerificationOverview>({
        method: "GET",
        url: "/contacts/verification",
        authorization: true,
    });
}

export async function requestContactVerification(req: VerificationRequest): Promise<VerificationResponse> {
    return await Request<VerificationResponse>({
        method: "POST",
        url: "/contacts/verification",
        data: req,
        authorization: true,
    });
}
