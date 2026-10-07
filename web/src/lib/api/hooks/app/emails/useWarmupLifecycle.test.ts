import { describe, expect, it } from "vitest";
import { applyWarmup } from "./useWarmupLifecycle";
import type Inbox from "@/lib/api/models/app/emails/Inbox";

describe("explicit diagnostic lifecycle", () => {
    const started = new Date("2026-01-02T12:00:00Z");
    const mailbox = { id: "mailbox", warmup: started, test_mode: null, shared_daily_limit: 8, rolling_recipient_limit: 11 } as Inbox;

    it("keeps untouched legacy input unchanged and transitions only on explicit action", () => {
        const row = applyWarmup(mailbox, "start");
        expect(mailbox.test_mode).toBeNull();
        expect(row).toMatchObject({ test_mode: "diagnostic", test_send_enabled: true, test_receive_enabled: true, warmup: started, shared_daily_limit: 8, rolling_recipient_limit: 11 });
    });

    it("pauses sending while preserving legacy receiving and stops both without resetting history", () => {
        expect(applyWarmup(mailbox, "pause")).toMatchObject({ test_mode: "diagnostic", test_send_enabled: false, test_receive_enabled: true, warmup: started });
        expect(applyWarmup(mailbox, "stop")).toMatchObject({ test_mode: "off", test_send_enabled: false, test_receive_enabled: false, warmup: started });
    });

    it("does not opt an explicit sender-only diagnostic into receiving when resumed", () => {
        const row = { ...mailbox, test_mode: "diagnostic", test_receive_enabled: false } as Inbox;
        expect(applyWarmup(row, "resume")).toMatchObject({ test_send_enabled: true, test_receive_enabled: false });
    });

    it("starts a new default-off mailbox only on an explicit start", () => {
        const row = { ...mailbox, test_mode: "off", test_send_enabled: false, test_receive_enabled: false, warmup: null } as Inbox;
        expect(applyWarmup(row, "pause")).toMatchObject({ test_mode: "diagnostic", test_send_enabled: false, test_receive_enabled: false, warmup: null });
        expect(applyWarmup(row, "start")).toMatchObject({ test_mode: "diagnostic", test_send_enabled: true, test_receive_enabled: true });
    });
});
