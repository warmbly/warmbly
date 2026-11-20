// Purchased Microsoft 365 mailboxes waiting on their administrator's approval, and what the return from Microsoft says.
import type { ConsentReason, Order } from "@/lib/api/models/app/store/Store";

export interface Waiting {
    domainId: string;
    domain: string;
    count: number;
    /** Why the automatic approval stopped, when known. */
    reason: ConsentReason | null;
}

/** The order's domains whose Microsoft 365 mailboxes wait on their administrator's approval. */
export function waitingForApproval(order: Order): Waiting[] {
    const byDomain = new Map<string, Waiting>();
    for (const m of order.mailboxes) {
        if (m.action_required !== "microsoft_consent") continue;
        const w = byDomain.get(m.domain_id);
        if (w) {
            w.count++;
            w.reason ??= m.action_reason ?? null;
            continue;
        }
        const domain = order.domains.find((d) => d.id === m.domain_id)?.name ?? m.email.slice(m.email.lastIndexOf("@") + 1);
        byDomain.set(m.domain_id, { domainId: m.domain_id, domain, count: 1, reason: m.action_reason ?? null });
    }
    return [...byDomain.values()];
}

/** What the order page says when the administrator comes back from Microsoft, from the status the return carries. */
export function approvalOutcome(status: string | null, domain: string | null): { kind: "success" | "info" | "error"; text: string } | null {
    const d = domain || "the domain";
    switch (status) {
        case "connected":
            return { kind: "success", text: `Approved. Connecting the mailboxes on ${d}.` };
        case "cancelled":
            return { kind: "info", text: `The approval for ${d} was cancelled. Nothing changed.` };
        case "error":
            return { kind: "error", text: `Microsoft did not confirm the approval for ${d}. Try again, signed in as the domain's admin mailbox.` };
        default:
            return null;
    }
}

/** One line on why Warmbly could not approve itself in the background, naming the admin mailbox when it is known. */
export function approvalReason(reason: ConsentReason | null, domain: string, admin: string | undefined): string {
    const who = admin ?? `the admin mailbox of ${domain}`;
    switch (reason) {
        case "mfa_required":
            return `Microsoft asked for an extra sign-in step for ${who}.`;
        case "wrong_password":
            return `Microsoft did not accept the saved password for ${who}.`;
        case "not_admin":
            return `${admin ?? "The admin mailbox"} is not a Global Administrator of the Microsoft 365 account for ${domain}.`;
        case "wrong_tenant":
            return `${admin ?? "The admin mailbox"} belongs to a different Microsoft 365 account than ${domain}.`;
        case "sign_in_blocked":
            return `Microsoft does not allow a background sign-in for ${who}.`;
        case "no_admin_sign_in":
            return `Warmbly could not read the sign-in for ${who}.`;
        default:
            return `Microsoft did not finish the approval in the background for ${domain}.`;
    }
}
