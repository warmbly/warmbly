import type MailboxAllowance from "./MailboxAllowance";
import type AddEmail from "./AddEmail";

export type BulkConnectStatus = "connected" | "skipped" | "failed";

/** One row's answer from POST /emails/onboarding/smtp-imap/bulk. */
export interface BulkConnectRow {
    /** Index into the batch that was sent, as the caller numbered it. */
    row: number;
    email: string;
    status: BulkConnectStatus;
    code?: string;
    message?: string;
    id?: string;
}

export interface BulkConnectResult {
    data: BulkConnectRow[];
    summary: { total: number; connected: number; skipped: number; failed: number };
    allowance?: MailboxAllowance;
}

export interface BulkConnectRequest {
    accounts: AddEmail[];
}
