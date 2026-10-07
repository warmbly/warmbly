import type Inbox from "@/lib/api/models/app/emails/Inbox";

type Participation = Pick<Inbox, "test_mode" | "test_send_enabled" | "test_receive_enabled">;

export function diagnosticSendingAllowed(row: Participation): boolean {
    return row.test_mode == null || row.test_mode === "legacy" || row.test_mode === "diagnostic" && row.test_send_enabled === true;
}

export function diagnosticWarmupActive(row: Participation & Pick<Inbox, "warmup" | "warmup_paused_at">): boolean {
    return diagnosticSendingAllowed(row) && !!row.warmup && !row.warmup_paused_at;
}
