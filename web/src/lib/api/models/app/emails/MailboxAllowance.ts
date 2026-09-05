import type LimitIncreaseRequest from "@/lib/api/models/app/organizations/LimitIncreaseRequest";

/** Where the allowance comes from; mirrors models.MailboxAllowanceBasis. */
export type MailboxAllowanceBasis = "unlimited" | "free" | "override" | "plan" | "fair_use";

/** GET /emails/allowance: how many mailboxes the workspace holds and may hold. */
export default interface MailboxAllowance {
    used: number;
    /** null means unlimited. */
    allowance: number | null;
    /** allowance minus used, never negative; null when unlimited. */
    remaining: number | null;
    basis: MailboxAllowanceBasis;
    /** The fair-use divisor: one mailbox for every this many daily sends. */
    sends_per_mailbox: number;
    plan_daily_sends?: number;
    plan_name?: string;
    paid: boolean;
    pending_request?: LimitIncreaseRequest | null;
}

/** True when the allowance is a real cap and it is full. */
export function allowanceFull(a: MailboxAllowance | undefined): boolean {
    return !!a && a.allowance != null && (a.remaining ?? 0) <= 0;
}
