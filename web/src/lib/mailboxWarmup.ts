import type Inbox from "@/lib/api/models/app/emails/Inbox";
import type AccountStatus from "@/lib/api/models/app/analytics/AccountStatus";
import type { WarmupStatusInfo } from "@/lib/api/models/app/analytics/AccountStatus";
import type { CloudLinkMailboxRow } from "@/lib/api/models/app/cloudlink/CloudLink";
import { diagnosticSendingAllowed } from "./diagnosticParticipation";

export interface WarmupSource {
    connected: boolean;
    loading?: boolean;
    unavailable?: boolean;
    reportsUnavailable?: boolean;
    rowFor: (id: string) => CloudLinkMailboxRow | undefined;
}

export type WarmupActivity = "active" | "paused" | "receive_only" | "off" | "inactive" | "connecting" | "removing" | "checking" | "unavailable";

export interface MailboxWarmup {
    source: "local" | "cloud" | "unknown";
    activity: WarmupActivity;
    cloudActivity?: WarmupActivity;
    info?: WarmupStatusInfo;
    sentToday?: number;
}

export const warmupActivityLabel: Record<WarmupActivity, string> = {
    active: "warming up", paused: "warmup paused", receive_only: "receiving warmup only",
    off: "warmup off", inactive: "warmup mailbox off", connecting: "Cloud enrollment pending",
    removing: "Cloud removal pending", checking: "warmup status checking", unavailable: "warmup status unavailable",
};

export function warmupUnknown(warmup: MailboxWarmup): boolean {
    return ["checking", "unavailable", "connecting", "removing"].includes(warmup.activity);
}

// An enrollment owns warmup even when its report is missing or its local anchor is absent.
export function resolveMailboxWarmup(mailbox: Pick<Inbox, "id" | "status" | "warmup" | "warmup_paused_at" | "test_mode" | "test_send_enabled" | "test_receive_enabled">, status?: AccountStatus, pool?: WarmupSource): MailboxWarmup {
    const row = pool?.connected ? pool.rowFor(mailbox.id) : undefined;
    if (row?.enrolled) {
        const source = "cloud" as const;
        if (row.enrollment_state === "pending_enroll") return { source, activity: "connecting" };
        if (row.enrollment_state === "pending_remove") return { source, activity: "removing" };
        const cloud = row.cloud;
        if (pool?.reportsUnavailable || !cloud) return { source, activity: "unavailable" };
        const info = cloud.warmup ? { ...cloud.warmup, current_volume: cloud.sent_today } : undefined;
        const values = { source, info, sentToday: cloud.sent_today };
        const cloudActivity: WarmupActivity = cloud.status && cloud.status !== "active" ? "inactive"
            : !info?.enabled || cloud.participation?.mode === "off" ? "off"
            : info.paused ? "paused"
            : cloud.participation && !cloud.participation.send ? cloud.participation.receive ? "receive_only" : "off"
            : "active";
        const activity = row.executor === "instance" && mailbox.status !== "active" ? "inactive" : cloudActivity;
        return { ...values, activity, cloudActivity };
    }
    if (pool?.loading) return { source: "unknown", activity: "checking" };
    if (pool?.unavailable) return { source: "unknown", activity: "unavailable" };
    const values = { source: "local" as const, info: status?.warmup_status, sentToday: status?.warmup_status?.current_volume ?? status?.daily_usage?.warmup_sent };
    if (mailbox.status !== "active") return { ...values, activity: "inactive" };
    if (!mailbox.warmup || mailbox.test_mode === "off") return { ...values, activity: "off" };
    if (mailbox.warmup_paused_at) return { ...values, activity: "paused" };
    if (!diagnosticSendingAllowed(mailbox)) return { ...values, activity: mailbox.test_receive_enabled ? "receive_only" : "off" };
    return { ...values, activity: "active" };
}
