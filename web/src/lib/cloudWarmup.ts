import type { WarmupSendFailure } from "@/lib/api/models/app/analytics/AccountStatus";
import type Inbox from "@/lib/api/models/app/emails/Inbox";
import type { PoolLinkMailboxState } from "@/lib/api/models/app/cloudlink/CloudLink";

// The cloud's open connection error names the cause better than the send it stopped.
export function cloudSendFailure(state?: PoolLinkMailboxState | null): WarmupSendFailure | undefined {
    const failure = state?.warmup?.send_failure;
    if (!failure) return undefined;
    const open = state?.errors?.[0];
    return open ? { ...failure, message: [open.title, open.message, open.action_required].filter(Boolean).join(". ") } : failure;
}

// Warmbly Cloud owns these for a mailbox it warms; an edit is sent to it and its answer is what shows.
export const cloudOwnedWarmupFields = [
    "warmup_base", "warmup_max", "warmup_increase", "warmup_reply_rate",
    "warmup_start_time", "warmup_end_time", "warmup_days",
] as const satisfies readonly (keyof Inbox)[];
