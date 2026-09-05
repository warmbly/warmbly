import Request from "../../Request";
import type AddEmail from "@/lib/api/models/app/emails/AddEmail";
import type { BulkConnectResult } from "@/lib/api/models/app/emails/BulkConnect";

/** The server caps a batch at this many rows; mirrors config.MailboxBulkBatchMax. */
export const BULK_CONNECT_BATCH = 50;

export default async function addEmailsBulk(accounts: AddEmail[]): Promise<BulkConnectResult> {
    return await Request<BulkConnectResult>({
        method: "POST",
        url: `/emails/onboarding/smtp-imap/bulk`,
        data: { accounts },
        authorization: true,
        // A batch dials every credential against a worker before answering.
        timeout: 180_000,
    });
}
