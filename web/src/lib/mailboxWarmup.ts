import type Inbox from "@/lib/api/models/app/emails/Inbox";
import type AccountStatus from "@/lib/api/models/app/analytics/AccountStatus";
import type { WarmupStatusInfo } from "@/lib/api/models/app/analytics/AccountStatus";
import type { CloudLinkMailboxRow, PoolLinkSelfExecution } from "@/lib/api/models/app/cloudlink/CloudLink";
import { diagnosticSendingAllowed } from "./diagnosticParticipation";

export interface WarmupSource {
    connected: boolean;
    loading?: boolean;
    unavailable?: boolean;
    reportsUnavailable?: boolean;
    /** Cloud enrollment is not readable by this member, so no mailbox can be confirmed local. */
    ownershipHidden?: boolean;
    /** Cloud's answer on whether this server may run its mailboxes' warmup; undefined when Cloud did not say. */
    selfExecutionGranted?: boolean;
    rowFor: (id: string) => CloudLinkMailboxRow | undefined;
}

export type WarmupActivity = "active" | "paused" | "receive_only" | "off" | "inactive" | "ungranted" | "unconfirmed" | "connecting" | "removing" | "checking" | "unavailable";

export interface MailboxWarmup {
    source: "local" | "cloud" | "unknown";
    activity: WarmupActivity;
    cloudActivity?: WarmupActivity;
    info?: WarmupStatusInfo;
    sentToday?: number;
}

export const warmupActivityLabel: Record<WarmupActivity, string> = {
    active: "warming up", paused: "warmup paused", receive_only: "receiving warmup only",
    off: "warmup off", inactive: "warmup mailbox off", ungranted: "waiting for permission to run on this server",
    unconfirmed: "permission to run on this server unconfirmed",
    connecting: "Cloud enrollment pending", removing: "Cloud removal pending",
    checking: "warmup status checking", unavailable: "warmup status unavailable",
};

export function warmupUnknown(warmup: MailboxWarmup): boolean {
    return ["checking", "unavailable", "unconfirmed", "connecting", "removing"].includes(warmup.activity);
}

// Cloud lifecycle controls need a Cloud report to act on; an unconfirmed grant still has one.
export function cloudReportMissing(warmup: MailboxWarmup): boolean {
    return warmup.cloudActivity === undefined;
}

// Undefined when Cloud did not answer about the grant; a lapsed expiry counts as withdrawn.
export function selfExecutionGranted(grant: PoolLinkSelfExecution | null | undefined, now = Date.now()): boolean | undefined {
    if (!grant) return undefined;
    return grant.granted && !(grant.expires_at && new Date(grant.expires_at).getTime() <= now);
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
        // This server runs the plan, so it only warms while the mailbox is on here and Cloud still grants it.
        const instance = row.executor === "instance";
        const activity = instance && mailbox.status !== "active" ? "inactive"
            : instance && cloudActivity === "active" && pool?.selfExecutionGranted !== true ? pool?.selfExecutionGranted === false ? "ungranted" : "unconfirmed"
            : cloudActivity;
        return { ...values, activity, cloudActivity };
    }
    if (pool?.loading) return { source: "unknown", activity: "checking" };
    if (pool?.unavailable || pool?.ownershipHidden) return { source: "unknown", activity: "unavailable" };
    const values = { source: "local" as const, info: status?.warmup_status, sentToday: status?.warmup_status?.current_volume ?? status?.daily_usage?.warmup_sent };
    if (mailbox.status !== "active") return { ...values, activity: "inactive" };
    if (!mailbox.warmup || mailbox.test_mode === "off") return { ...values, activity: "off" };
    if (mailbox.warmup_paused_at) return { ...values, activity: "paused" };
    if (!diagnosticSendingAllowed(mailbox)) return { ...values, activity: mailbox.test_receive_enabled ? "receive_only" : "off" };
    return { ...values, activity: "active" };
}
