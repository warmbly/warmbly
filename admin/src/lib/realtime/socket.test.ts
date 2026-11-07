import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

vi.mock("@/lib/api/client", () => ({
    Request: vi.fn(async () => ({ url: "ws://localhost:4000/socket/websocket", expires_in: 600 })),
    SessionExpiredError: class SessionExpiredError extends Error {},
}));

import { Request, SessionExpiredError } from "@/lib/api/client";
import { startRealtime, stopRealtime, getStatus, getRefusal, onEvent, refusalMessage } from "./socket";

const TOPIC = "admin:platform";

interface Frame {
    topic: string;
    event: string;
    payload: Record<string, unknown>;
    ref?: string;
    join_ref?: string;
}

let sent: Frame[];
let sockets: FakeWS[];

class FakeWS {
    static CONNECTING = 0;
    static OPEN = 1;
    static CLOSING = 2;
    static CLOSED = 3;

    readyState = FakeWS.OPEN;
    onopen: ((e: unknown) => void) | null = null;
    onmessage: ((e: { data: string }) => void) | null = null;
    onclose: ((e: unknown) => void) | null = null;
    onerror: ((e: unknown) => void) | null = null;

    constructor(public url: string) {
        sockets.push(this);
        setTimeout(() => this.onopen?.({}), 0);
    }

    send(data: string) {
        const msg = JSON.parse(data) as Frame;
        sent.push(msg);
        // Answer heartbeats, or the zombie watchdog closes the socket and
        // reconnects under every test that runs longer than 25s of fake time.
        if (msg.topic === "phoenix" && msg.event === "heartbeat") {
            setTimeout(() => {
                this.onmessage?.({
                    data: JSON.stringify({
                        topic: "phoenix",
                        event: "phx_reply",
                        ref: msg.ref,
                        payload: { status: "ok", response: {} },
                    }),
                });
            }, 0);
        }
    }

    close() {
        if (this.readyState === FakeWS.CLOSED) return;
        this.readyState = FakeWS.CLOSED;
        this.onclose?.({ wasClean: true });
    }
}

const joins = () => sent.filter((m) => m.event === "phx_join");
const lastJoin = () => joins()[joins().length - 1];

function deliver(msg: Record<string, unknown>) {
    sockets[sockets.length - 1]?.onmessage?.({ data: JSON.stringify(msg) });
}

function refuseJoin(response: Record<string, unknown>) {
    const join = lastJoin();
    deliver({
        topic: TOPIC,
        event: "phx_reply",
        ref: join.ref,
        join_ref: join.join_ref,
        payload: { status: "error", response },
    });
}

function ackJoin() {
    const join = lastJoin();
    deliver({
        topic: TOPIC,
        event: "phx_reply",
        ref: join.ref,
        join_ref: join.join_ref,
        payload: { status: "ok", response: {} },
    });
}

const tick = (ms: number) => vi.advanceTimersByTimeAsync(ms);

beforeEach(async () => {
    sent = [];
    sockets = [];
    vi.useFakeTimers();
    vi.spyOn(Math, "random").mockReturnValue(0);
    (globalThis as unknown as { WebSocket: unknown }).WebSocket = FakeWS;
    startRealtime();
    await tick(10);
});

afterEach(() => {
    stopRealtime();
    vi.useRealTimers();
    vi.restoreAllMocks();
});

describe("joining the admin channel", () => {
    it("only reports connected once the channel is joined", async () => {
        expect(getStatus()).toBe("connecting");

        ackJoin();
        expect(getStatus()).toBe("connected");
    });

    it("waits out a throttled join instead of tearing the socket down", async () => {
        const socketsBefore = sockets.length;

        refuseJoin({ code: 4007, reason: "rate_limited", retry_after_ms: 5000 });

        // Same socket, no reconnect.
        expect(sockets).toHaveLength(socketsBefore);
        expect(joins()).toHaveLength(1);

        await tick(4000);
        expect(joins()).toHaveLength(1);

        await tick(1500);
        expect(joins()).toHaveLength(2);
        expect(sockets).toHaveLength(socketsBefore);

        ackJoin();
        expect(getStatus()).toBe("connected");
    });

    it("does not storm the server when a join is refused for good", async () => {
        // A non-admin (or any permanent refusal) used to reconnect on the
        // 120ms floor forever, because the backoff reset on socket open.
        for (let i = 0; i < 12; i++) {
            if (lastJoin()) refuseJoin({ code: 4010, reason: "not_an_admin" });
            await tick(5000);
        }

        // With the flat 120ms retry this window would be hundreds of attempts.
        expect(joins().length).toBeLessThanOrEqual(12);
        expect(sockets.length).toBeLessThanOrEqual(12);
    });

    it.each(["admin_mfa_required", "not_an_admin", "jwt_required"])(
        "stops for good on a %s refusal and explains it",
        async (reason) => {
            refuseJoin({ reason });
            await tick(60_000);
            window.dispatchEvent(new Event("focus"));
            await tick(60_000);

            expect(joins()).toHaveLength(1);
            expect(sockets).toHaveLength(1);
            expect(getStatus()).toBe("refused");
            expect(refusalMessage(getRefusal())).toBeTruthy();
        },
    );

    it("gives up on a throttled join rather than retrying forever", async () => {
        for (let i = 0; i < 20; i++) {
            if (lastJoin()) refuseJoin({ reason: "rate_limited", retry_after_ms: 1000 });
            await tick(2000);
        }

        expect(joins().length).toBeLessThanOrEqual(9);
        expect(getStatus()).toBe("disconnected");
    });

    it("clamps an absurd retry hint", async () => {
        refuseJoin({ reason: "rate_limited", retry_after_ms: 999_999_999 });

        await tick(61_000);
        expect(joins()).toHaveLength(2);
    });

    it("backs off further on each channel crash", async () => {
        ackJoin();

        const delays: number[] = [];
        for (let i = 0; i < 3; i++) {
            const before = joins().length;
            deliver({ topic: TOPIC, event: "phx_error", payload: {} });

            let waited = 0;
            while (joins().length === before && waited < 90_000) {
                await tick(100);
                waited += 100;
            }
            delays.push(waited);
            ackJoin();
        }

        expect(delays).toEqual([1000, 2000, 5000]);
    });

    it("starts the backoff over once the channel has been healthy for a while", async () => {
        ackJoin();

        const measure = async () => {
            const before = joins().length;
            deliver({ topic: TOPIC, event: "phx_error", payload: {} });
            let waited = 0;
            while (joins().length === before && waited < 90_000) {
                await tick(100);
                waited += 100;
            }
            return waited;
        };

        expect(await measure()).toBe(1000);
        ackJoin();

        await tick(45_000);

        expect(await measure()).toBe(1000);
    });
});

describe("event delivery", () => {
    it("forwards platform events to subscribers", async () => {
        ackJoin();
        const seen: Array<[string, Record<string, unknown>]> = [];
        const off = onEvent((name, payload) => seen.push([name, payload]));

        deliver({ topic: TOPIC, event: "AUDIT_CREATED", payload: { entity_type: "campaign" } });

        expect(seen).toEqual([["AUDIT_CREATED", { entity_type: "campaign" }]]);
        off();
    });

    it("never forwards Phoenix internals", async () => {
        ackJoin();
        const seen: string[] = [];
        const off = onEvent((name) => seen.push(name));

        deliver({ topic: TOPIC, event: "presence_diff", payload: {} });
        deliver({ topic: TOPIC, event: "phx_close", payload: {} });

        expect(seen).toEqual([]);
        off();
    });
});

describe("status reporting", () => {
    it("stops reporting connected once the socket closes", async () => {
        ackJoin();
        expect(getStatus()).toBe("connected");

        sockets[sockets.length - 1].close();
        await tick(1);

        expect(getStatus()).toBe("disconnected");
    });
});

describe("an expired session", () => {
    it("stops asking for socket URLs and logs nothing as an error", async () => {
        const errors = vi.spyOn(console, "error").mockImplementation(() => {});
        vi.mocked(Request).mockClear();
        vi.mocked(Request).mockRejectedValueOnce(new SessionExpiredError());

        sockets[sockets.length - 1].close();
        await tick(60_000);
        window.dispatchEvent(new Event("focus"));
        window.dispatchEvent(new Event("online"));
        await tick(60_000);

        expect(Request).toHaveBeenCalledTimes(1);
        expect(errors).not.toHaveBeenCalled();
        expect(getStatus()).toBe("disconnected");
    });

    it("connects again after the next sign-in", async () => {
        vi.mocked(Request).mockRejectedValueOnce(new SessionExpiredError());
        sockets[sockets.length - 1].close();
        await tick(1_000);
        const before = sockets.length;

        stopRealtime();
        startRealtime();
        await tick(10);

        expect(sockets.length).toBe(before + 1);
    });
});
