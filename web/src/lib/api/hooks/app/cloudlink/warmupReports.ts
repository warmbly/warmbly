import type { InfiniteData, QueryClient } from "@tanstack/react-query";
import type GetEmails from "@/lib/api/models/app/emails/GetEmails";
import type Inbox from "@/lib/api/models/app/emails/Inbox";
import type { CloudLinkMailboxRow } from "@/lib/api/models/app/cloudlink/CloudLink";

export const CLOUD_LINK_KEY = ["cloud-link"];

// Capture the scope before the write, since the user can switch workspaces while it is pending.
export function cloudLinkScope(client: QueryClient): readonly unknown[] {
    const organization = client.getQueryData<{ id: string }>(["organizations", "current"]);
    return organization?.id ? [...CLOUD_LINK_KEY, organization.id] : CLOUD_LINK_KEY;
}

export function cloudLinkScopeCurrent(client: QueryClient, scope: readonly unknown[]): boolean {
    return scope[1] === cloudLinkScope(client)[1];
}

// Enrolling or removing changes which pool a mailbox's warmup analytics and placement come from.
export async function invalidateWarmupReports(client: QueryClient, scope: readonly unknown[]) {
    const current = cloudLinkScopeCurrent(client, scope);
    await Promise.all([
        client.invalidateQueries({ queryKey: scope }),
        current && client.invalidateQueries({ queryKey: ["analytics", "accounts"] }),
        current && client.invalidateQueries({ queryKey: ["analytics", "warmup"] }),
    ]);
}

const clock = (value?: string) => (value ?? "").slice(0, 5);

// Listing Cloud mailboxes also mirrors Cloud's warmup settings onto them, so a cached mailbox that disagrees is out of date.
export function mirroredSettingsStale(client: QueryClient, rows: readonly CloudLinkMailboxRow[]): boolean {
    const cached = new Map<string, Inbox>();
    for (const [, data] of client.getQueriesData<InfiniteData<GetEmails> | Inbox[]>({ queryKey: ["emails", "list"] })) {
        const list = Array.isArray(data) ? data : data?.pages?.flatMap((page) => page.data ?? []) ?? [];
        for (const inbox of list) cached.set(inbox.id, inbox);
    }
    return rows.some((row) => {
        const settings = row.cloud?.settings;
        const inbox = cached.get(row.id);
        if (!row.enrolled || row.enrollment_state === "pending_remove" || !settings || !clock(settings.start_time) || !clock(settings.end_time) || !inbox) return false;
        // Out-of-range reports are not mirrored, so they would never match.
        if ([settings.base, settings.max, settings.increase, settings.reply_rate].some((v) => v < 0 || v > 100)) return false;
        return inbox.warmup_base !== settings.base || inbox.warmup_max !== settings.max || inbox.warmup_increase !== settings.increase
            || inbox.warmup_reply_rate !== settings.reply_rate || (inbox.warmup_days ?? settings.days) !== settings.days
            || clock(inbox.warmup_start_time) !== clock(settings.start_time) || clock(inbox.warmup_end_time) !== clock(settings.end_time);
    });
}
