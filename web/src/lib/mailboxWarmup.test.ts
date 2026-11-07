import { describe, expect, it } from "vitest";
import type Inbox from "@/lib/api/models/app/emails/Inbox";
import type AccountStatus from "@/lib/api/models/app/analytics/AccountStatus";
import type { CloudLinkMailboxRow, PoolLinkMailboxState } from "@/lib/api/models/app/cloudlink/CloudLink";
import { cloudReportMissing, resolveMailboxWarmup, selfExecutionGranted, warmupUnknown } from "./mailboxWarmup";

const mailbox = { id: "mailbox", status: "active", warmup: new Date("2026-01-01") } as Inbox;
const report = { status: "active", sent_today: 5, warmup: { enabled: true, paused: false, current_volume: 0, target_volume: 12, days_active: 3 } } as PoolLinkMailboxState;
const resolve = (row: Partial<CloudLinkMailboxRow>, local = mailbox) => resolveMailboxWarmup(local, undefined, { connected: true, rowFor: () => ({ id: local.id, enrolled: true, ...row } as CloudLinkMailboxRow) });

describe("warmup source and lifecycle", () => {
    it("does not count an instance executor as warming once Cloud withdraws its grant, keeping the reported lifecycle", () => {
        const pool = (granted?: boolean) => ({ connected: true, selfExecutionGranted: granted, rowFor: () => ({ id: mailbox.id, enrolled: true, executor: "instance", cloud: report } as CloudLinkMailboxRow) });
        expect(resolveMailboxWarmup(mailbox, undefined, pool(false))).toMatchObject({ source: "cloud", activity: "ungranted", cloudActivity: "active" });
        expect(resolveMailboxWarmup(mailbox, undefined, pool(true)).activity).toBe("active");
        expect(resolveMailboxWarmup(mailbox, undefined, pool(undefined))).toMatchObject({ activity: "unconfirmed", cloudActivity: "active" });
        expect(warmupUnknown(resolveMailboxWarmup(mailbox, undefined, pool(undefined)))).toBe(true);
        expect(cloudReportMissing(resolveMailboxWarmup(mailbox, undefined, pool(undefined)))).toBe(false);
        expect(resolve({ executor: "cloud", cloud: report }).activity).toBe("active");
    });
    it("reads a lapsed grant expiry as withdrawn and an absent answer as unknown", () => {
        const now = Date.parse("2026-11-07T12:00:00Z");
        expect(selfExecutionGranted({ protocol: 1, granted: true, expires_at: new Date("2026-11-07T11:00:00Z") }, now)).toBe(false);
        expect(selfExecutionGranted({ protocol: 1, granted: true, expires_at: new Date("2026-11-08T11:00:00Z") }, now)).toBe(true);
        expect(selfExecutionGranted({ protocol: 1, granted: false }, now)).toBe(false);
        expect(selfExecutionGranted(undefined, now)).toBeUndefined();
    });
    it("keeps a mailbox unknown rather than local when this member cannot read Cloud enrollment", () => {
        expect(resolveMailboxWarmup({ ...mailbox, warmup: undefined }, undefined, { connected: false, ownershipHidden: true, rowFor: () => undefined })).toEqual({ source: "unknown", activity: "unavailable" });
    });
    it.each([undefined, new Date("2026-01-01")])("uses Cloud activity and counters independently of the local start order (%s)", (warmup) => {
        const local = { ...mailbox, warmup, warmup_paused_at: new Date(), test_mode: "off" as const, status: "inactive" };
        expect(resolve({ cloud: report }, local)).toMatchObject({ source: "cloud", activity: "active", sentToday: 5, info: { current_volume: 5, target_volume: 12 } });
    });
    it("does not count a switched-off instance executor as warming even if Cloud reports an active lifecycle", () => {
        const local = { ...mailbox, status: "inactive" };
        expect(resolve({ executor: "instance", cloud: report }, local)).toMatchObject({ source: "cloud", activity: "inactive", cloudActivity: "active", sentToday: 5 });
        expect(resolve({ executor: "instance", cloud: { ...report, warmup: { ...report.warmup!, paused: true } } }, local)).toMatchObject({ activity: "inactive", cloudActivity: "paused" });
        expect(resolve({ executor: "cloud", cloud: report }, local).activity).toBe("active");
    });
    it.each([
        ["paused", { ...report, warmup: { ...report.warmup!, paused: true } }],
        ["off", { ...report, warmup: null }],
        ["off", { ...report, warmup: { ...report.warmup!, enabled: false } }],
        ["off", { ...report, participation: { mode: "off", send: false, receive: false } }],
        ["receive_only", { ...report, participation: { mode: "diagnostic", send: false, receive: true } }],
        ["off", { ...report, participation: { mode: "diagnostic", send: false, receive: false } }],
        ["inactive", { ...report, status: "inactive" }],
        ["unavailable", null],
    ] as const)("reports %s from the remote lifecycle", (activity, cloud) => {
        expect(resolve({ cloud: cloud as PoolLinkMailboxState | null }).activity).toBe(activity);
    });
    it.each([["pending_enroll", "connecting"], ["pending_remove", "removing"]] as const)("never falls back locally during %s", (enrollment_state, activity) => {
        expect(resolve({ enrollment_state, cloud: report })).toEqual({ source: "cloud", activity });
    });
    it("keeps missing reports unknown even in successful responses and failed refetches", () => {
        expect(resolve({ cloud: null })).toEqual({ source: "cloud", activity: "unavailable" });
        expect(resolveMailboxWarmup(mailbox, undefined, { connected: true, reportsUnavailable: true, rowFor: () => ({ enrolled: true, cloud: report } as CloudLinkMailboxRow) })).toEqual({ source: "cloud", activity: "unavailable" });
    });
    it.each([[true, false, "checking"], [false, true, "unavailable"]] as const)("does not confirm local warmup during bootstrap or a source error", (loading, unavailable, activity) => {
        expect(resolveMailboxWarmup(mailbox, undefined, { connected: false, loading, unavailable, rowFor: () => undefined })).toEqual({ source: "unknown", activity });
    });
    it("accepts an older Cloud report without enrollment or participation fields without inventing consent", () => {
        expect(resolve({ cloud: report }).activity).toBe("active");
        expect(resolve({ cloud: report }).info).not.toHaveProperty("participation");
    });
    it("does not substitute local scheduling or partner limits for absent Cloud fields", () => {
        const local = { warmup_status: { current_volume: 0, next_send_at: new Date(), partner_limit: { reachable: 0, ramp_target: 0 } } } as AccountStatus;
        const state = resolveMailboxWarmup(mailbox, local, { connected: true, rowFor: () => ({ enrolled: true, cloud: report } as CloudLinkMailboxRow) });
        expect(state.info).not.toHaveProperty("next_send_at");
        expect(state.info).not.toHaveProperty("partner_limit");
        expect(state.info?.current_volume).toBe(5);
    });
    it.each([
        [{ ...mailbox }, "active"],
        [{ ...mailbox, status: "inactive" }, "inactive"],
        [{ ...mailbox, warmup: undefined }, "off"],
        [{ ...mailbox, warmup_paused_at: new Date() }, "paused"],
        [{ ...mailbox, test_mode: "off" }, "off"],
        [{ ...mailbox, test_mode: "diagnostic", test_send_enabled: false, test_receive_enabled: true }, "receive_only"],
    ] as const)("retains local lifecycle semantics for %j", (local, activity) => {
        expect(resolveMailboxWarmup(local).activity).toBe(activity);
    });
});
