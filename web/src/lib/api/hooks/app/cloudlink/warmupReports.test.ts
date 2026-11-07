import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";
import type Inbox from "@/lib/api/models/app/emails/Inbox";
import type { CloudLinkMailboxRow } from "@/lib/api/models/app/cloudlink/CloudLink";
import { invalidateWarmupReports, mirroredSettingsStale } from "./warmupReports";

const settings = { base: 10, max: 40, increase: 2, reply_rate: 30, start_time: "09:00", end_time: "17:00", days: 31, timezone: "UTC" };
const inbox = { id: "mailbox", warmup_base: 10, warmup_max: 40, warmup_increase: 2, warmup_reply_rate: 30, warmup_start_time: "09:00:00", warmup_end_time: "17:00:00", warmup_days: 31 } as Inbox;
const row = (patch: Partial<typeof settings> = {}) => ({ id: "mailbox", enrolled: true, enrollment_state: "active", cloud: { settings: { ...settings, ...patch } } }) as unknown as CloudLinkMailboxRow;

function clientWith(rows: Inbox[]) {
    const client = new QueryClient();
    client.setQueryData(["organizations", "current"], { id: "workspace" });
    client.setQueryData(["emails", "list", "", "", 100], { pages: [{ data: rows }], pageParams: [undefined] });
    return client;
}

describe("Cloud report cache freshness", () => {
    it("refreshes warmup analytics and placement alongside the Cloud report after an ownership change", async () => {
        const client = clientWith([inbox]);
        for (const key of [["analytics", "warmup", "mailbox", "", ""], ["analytics", "warmup", "placement", "mailbox", "a", "b"], ["analytics", "accounts", "mailbox"], ["cloud-link", "workspace", "mailboxes"], ["cloud-link", "other", "mailboxes"]]) {
            client.setQueryData(key, {});
        }
        await invalidateWarmupReports(client, ["cloud-link", "workspace"]);
        expect(client.getQueryState(["analytics", "warmup", "mailbox", "", ""])?.isInvalidated).toBe(true);
        expect(client.getQueryState(["analytics", "warmup", "placement", "mailbox", "a", "b"])?.isInvalidated).toBe(true);
        expect(client.getQueryState(["cloud-link", "other", "mailboxes"])?.isInvalidated).toBe(false);
    });

    it("leaves another workspace's analytics alone when the write's workspace is no longer current", async () => {
        const client = clientWith([inbox]);
        client.setQueryData(["analytics", "warmup", "mailbox", "", ""], {});
        await invalidateWarmupReports(client, ["cloud-link", "earlier"]);
        expect(client.getQueryState(["analytics", "warmup", "mailbox", "", ""])?.isInvalidated).toBe(false);
    });

    it("flags a cached mailbox whose warmup settings disagree with the mirrored Cloud report", () => {
        const client = clientWith([inbox]);
        expect(mirroredSettingsStale(client, [row()])).toBe(false);
        expect(mirroredSettingsStale(client, [row({ max: 35 })])).toBe(true);
        expect(mirroredSettingsStale(client, [row({ start_time: "08:00" })])).toBe(true);
        expect(mirroredSettingsStale(client, [{ ...row({ max: 35 }), enrollment_state: "pending_remove" }])).toBe(false);
        expect(mirroredSettingsStale(client, [row({ max: 500 })])).toBe(false);
    });
});
