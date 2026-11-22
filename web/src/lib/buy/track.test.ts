import { describe, expect, it } from "vitest";
import type { Order, OrderStep, StepKey, StepStatus } from "@/lib/api/models/app/store/Store";
import { activityLine, deriveTrack, ORDER_POLL_LIVE_MS, ORDER_POLL_OFFLINE_MS, orderPollMs, stepCount, stepsDone, STEP_KEYS } from "./track";

function order(status: Order["status"], states: Partial<Record<StepKey, StepStatus>>, extra: Partial<Order> = {}): Order {
    const steps: OrderStep[] = STEP_KEYS.map((key) => ({ key, status: states[key] ?? "pending", at: states[key] === "done" ? "2026-11-16T10:00:00Z" : null }));
    return {
        id: "o1",
        status,
        created_at: "2026-11-16T09:59:00Z",
        completed_at: null,
        due_now_micros: 0,
        charged_micros: 0,
        refunded_micros: 0,
        steps,
        domains: [{ id: "d1", name: "getacme.com", status: "registering", provider: "google", expires_at: null, auto_renew: true, renew_micros: 0, forwarding_url: null, mail_auth: null, mailbox_count: 3, created_at: "", source: "purchased", renewable: false, forwarding_status: "none", nameservers: [], connection: null, dns_conflicts: [], owner_verification: null, transfer: null, tracking_host: "" }],
        mailboxes: [],
        ...extra,
    };
}

describe("deriveTrack", () => {
    it("shows a paying state before the order has steps", () => {
        const t = deriveTrack(undefined);
        expect(t.phase).toBe("paying");
        expect(t.rows.every((r) => r.state === "pending")).toBe(true);
        expect(t.progress).toBe(0);
    });

    it("says what the running step is doing, with names and counts", () => {
        const t = deriveTrack(order("processing", { register: "running" }), { mailboxes: 3 });
        expect(t.activeIndex).toBe(0);
        expect(t.headline).toBe("Registering getacme.com…");
        expect(t.rows[3].title).toBe("Create 3 mailboxes");
    });

    it("treats the first unfinished step as under way while processing", () => {
        const t = deriveTrack(order("processing", { register: "done", dns: "done" }));
        expect(t.activeIndex).toBe(2);
        expect(t.rows[2]).toMatchObject({ state: "active", title: "Publishing SPF, DKIM and DMARC" });
        expect(t.rows[0].at).toBeInstanceOf(Date);
        // Two nodes done draws one full segment and half of the next, out of five.
        expect(t.progress).toBeCloseTo(1.5 / 5);
    });

    it("counts skipped steps as passed", () => {
        const t = deriveTrack(order("processing", { register: "done", dns: "done", mail_auth: "done", mailboxes: "done", delegation: "skipped", ready: "running" }));
        expect(t.activeIndex).toBe(5);
        expect(t.progress).toBeCloseTo(4.5 / 5);
    });

    it("finishes every step and the line on a completed order", () => {
        const done = order("completed", { register: "done", dns: "done", mail_auth: "done", mailboxes: "done", delegation: "done", ready: "running" }, {
            mailboxes: [1, 2, 3].map((i) => ({
                id: `m${i}`,
                email: `a${i}@getacme.com`,
                domain_id: "d1",
                provider: "google",
                status: "active",
                first_name: "Ada",
                last_name: "Lovelace",
                monthly_micros: 3_000_000,
                paid_through: null,
                cancels_at: null,
                email_account_id: null,
                connect_state: "connected",
                is_admin: i === 1,
                paused: false,
                created_at: "",
            })),
        });
        const t = deriveTrack(done);
        expect(t.phase).toBe("done");
        expect(t.progress).toBe(1);
        expect(t.rows.every((r) => r.state === "done")).toBe(true);
        expect(t.headline).toBe("3 mailboxes are ready");
    });

    it("reports a partial failure", () => {
        const t = deriveTrack(order("processing", { register: "done", dns: "failed" }));
        expect(t.phase).toBe("partial");
        expect(t.rows[1].state).toBe("failed");
        expect(deriveTrack(order("failed", { register: "failed" })).phase).toBe("failed");
    });
});

describe("orderPollMs", () => {
    const mailbox = (connect_state: "waiting" | "connecting" | "connected" | "failed") => ({ connect_state }) as Order["mailboxes"][number];

    it("polls every 2s while the order moves and the socket is down, and every 30s while it is live", () => {
        expect(orderPollMs(order("processing", { register: "running" }), false)).toBe(ORDER_POLL_OFFLINE_MS);
        expect(orderPollMs(order("charging", {}), false)).toBe(2_000);
        expect(orderPollMs(order("processing", { register: "running" }), true)).toBe(ORDER_POLL_LIVE_MS);
        expect(orderPollMs(order("processing", {}), true)).toBe(30_000);
    });

    it("keeps polling a finished order while a mailbox is still connecting, then stops", () => {
        expect(orderPollMs(order("completed", {}, { mailboxes: [mailbox("connected"), mailbox("connecting")] }), false)).toBe(2_000);
        expect(orderPollMs(order("completed", {}, { mailboxes: [mailbox("waiting")] }), true)).toBe(30_000);
        expect(orderPollMs(order("completed", {}, { mailboxes: [mailbox("connected"), mailbox("failed")] }), false)).toBe(false);
        expect(orderPollMs(order("failed", {}), false)).toBe(false);
    });

    it("polls an order not loaded yet only while the socket is down", () => {
        expect(orderPollMs(undefined, false)).toBe(2_000);
        expect(orderPollMs(undefined, true)).toBe(false);
    });
});

describe("live step counts", () => {
    it("counts domains registered and mailboxes created and connected", () => {
        const base = order("processing", { register: "running" });
        const d = base.domains[0];
        const o = { ...base, domains: [d, { ...d, id: "d2", name: "tryacme.com", status: "active" }] };
        expect(stepCount(o, "register")).toEqual({ done: 1, total: 2, noun: "registered" });
        const m = (status: string, connect_state: "waiting" | "connected") => ({ status, connect_state }) as Order["mailboxes"][number];
        const boxes = { ...o, mailboxes: [m("active", "connected"), m("active", "waiting"), m("provisioning", "waiting")] };
        expect(stepCount(boxes, "mailboxes")).toEqual({ done: 2, total: 3, noun: "created" });
        expect(stepCount(boxes, "delegation")).toEqual({ done: 1, total: 3, noun: "connected" });
        // One domain needs no counter.
        expect(stepCount(base, "register")).toBeNull();
        expect(stepCount(o, "dns")).toBeNull();
    });

    it("counts finished steps and says what is happening now", () => {
        const t = deriveTrack(order("processing", { register: "done", dns: "done", mail_auth: "running" }));
        expect(stepsDone(t)).toBe(2);
        expect(activityLine(t, ["getacme.com"])).toMatch(/records/);
        expect(activityLine(deriveTrack(undefined), [])).toMatch(/payment/);
    });
});
