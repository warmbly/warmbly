import type Inbox from "@/lib/api/models/app/emails/Inbox";

export type MailboxDisplayStatus = "healthy" | "warming" | "paused" | "inactive";

// The API returns the raw account status (active | inactive | revoked); the
// lifecycle the UI talks about is derived from the warmup fields. An active
// account mid-ramp is "warming"; a finished ramp, or an active account that
// isn't warming, is "healthy".
export default function mailboxDisplayStatus(box: Inbox): MailboxDisplayStatus {
    if (box.status !== "active") return "inactive";
    if (box.warmup && box.warmup_paused_at) return "paused";
    if (box.warmup) {
        const days = Math.floor(
            (Date.now() - new Date(box.warmup).getTime()) / 86_400_000,
        );
        const target = box.warmup_base + days * box.warmup_increase;
        if (target < box.warmup_max) return "warming";
    }
    return "healthy";
}
