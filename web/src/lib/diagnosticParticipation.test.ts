import { describe, expect, it } from "vitest";
import type Inbox from "@/lib/api/models/app/emails/Inbox";
import { diagnosticSendingAllowed, diagnosticWarmupActive } from "./diagnosticParticipation";
import mailboxDisplayStatus from "./mailboxStatus";
import { resolveMailboxWarmup } from "./mailboxWarmup";
import type { CloudLinkMailboxRow, PoolLinkMailboxState } from "@/lib/api/models/app/cloudlink/CloudLink";

describe("consent-aware mailbox activity", () => {
    const legacy = { status: "active", warmup: new Date("2020-01-01"), warmup_paused_at: null, test_mode: null } as Inbox;

    it("preserves old NULL/legacy activity without presenting age as readiness", () => {
        expect(diagnosticWarmupActive(legacy)).toBe(true);
        expect(diagnosticSendingAllowed({ ...legacy, test_mode: "legacy" })).toBe(true);
        expect(mailboxDisplayStatus(legacy)).toBe("warming");
    });

    it("never displays off or receive-only accounts as sending diagnostics despite old warmup timestamps", () => {
        const off = { ...legacy, test_mode: "off" } as Inbox;
        const receiver = { ...legacy, test_mode: "diagnostic", test_send_enabled: false, test_receive_enabled: true } as Inbox;
        expect(diagnosticWarmupActive(off)).toBe(false);
        expect(diagnosticWarmupActive(receiver)).toBe(false);
        expect(mailboxDisplayStatus(receiver)).toBe("paused");
        expect(mailboxDisplayStatus(off)).not.toBe("warming");
        expect(diagnosticWarmupActive({ ...receiver, test_send_enabled: true })).toBe(true);
    });

    it("respects Cloud off/receive-only while preserving unsupported legacy shape", () => {
        const old = { warmup: { enabled: true, paused: false } } as PoolLinkMailboxState;
        const activity = (cloud: PoolLinkMailboxState) => resolveMailboxWarmup(legacy, undefined, { connected: true, rowFor: () => ({ enrolled: true, cloud } as CloudLinkMailboxRow) }).activity;
        expect(activity(old)).toBe("active");
        expect(activity({ ...old, participation: { mode: "off", send: false, receive: false } })).toBe("off");
        expect(activity({ ...old, participation: { mode: "diagnostic", send: false, receive: true } })).toBe("receive_only");
    });
});
