import type { WarmupSendFailure } from "@/lib/api/models/app/analytics/AccountStatus";
import type { PoolLinkMailboxState } from "@/lib/api/models/app/cloudlink/CloudLink";

// The cloud's open connection error names the cause better than the send it stopped.
export function cloudSendFailure(state?: PoolLinkMailboxState | null): WarmupSendFailure | undefined {
    const failure = state?.warmup?.send_failure;
    if (!failure) return undefined;
    const open = state?.errors?.[0];
    return open ? { ...failure, message: [open.title, open.message, open.action_required].filter(Boolean).join(". ") } : failure;
}

// Not warming on the cloud: paused, or reported with warmup never started there. Resume starts either.
export function cloudWarmupPaused(state?: PoolLinkMailboxState | null): boolean {
    return !!state && (!state.warmup || state.warmup.paused || !!state.participation && (state.participation.mode === "off" || !state.participation.send));
}
