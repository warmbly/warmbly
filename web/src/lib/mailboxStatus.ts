import type Inbox from "@/lib/api/models/app/emails/Inbox";
import { diagnosticSendingAllowed, diagnosticWarmupActive } from "./diagnosticParticipation";

export type MailboxDisplayStatus = "healthy" | "warming" | "paused" | "inactive";

// Account activity is not proof of deliverability or reputation readiness.
export default function mailboxDisplayStatus(box: Inbox): MailboxDisplayStatus {
    if (box.status !== "active") return "inactive";
    if (box.test_mode !== "off" && box.warmup && (box.warmup_paused_at || !diagnosticSendingAllowed(box))) return "paused";
    if (diagnosticWarmupActive(box)) return "warming";
    return "healthy";
}
