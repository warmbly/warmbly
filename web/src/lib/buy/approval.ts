// Purchased Microsoft 365 mailboxes waiting on their administrator's approval, and what the return from Microsoft says.
import type { Order } from "@/lib/api/models/app/store/Store";

export interface Waiting {
    domainId: string;
    domain: string;
    count: number;
}

/** The order's domains whose Microsoft 365 mailboxes wait on their administrator's approval. */
export function waitingForApproval(order: Order): Waiting[] {
    const byDomain = new Map<string, Waiting>();
    for (const m of order.mailboxes) {
        if (m.action_required !== "microsoft_consent") continue;
        const w = byDomain.get(m.domain_id);
        if (w) {
            w.count++;
            continue;
        }
        const domain = order.domains.find((d) => d.id === m.domain_id)?.name ?? m.email.slice(m.email.lastIndexOf("@") + 1);
        byDomain.set(m.domain_id, { domainId: m.domain_id, domain, count: 1 });
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
