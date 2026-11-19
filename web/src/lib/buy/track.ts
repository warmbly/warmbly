// The setup track: the order's six fixed steps, turned into what each row says and how far the line has drawn.
import type { Order, OrderStep, StepKey, StepStatus } from "@/lib/api/models/app/store/Store";

export const STEP_KEYS: StepKey[] = ["register", "dns", "mail_auth", "mailboxes", "delegation", "ready"];

export type TrackState = "pending" | "active" | "done" | "failed" | "skipped";
export type TrackPhase = "paying" | "working" | "done" | "partial" | "failed";

export interface TrackRow {
    key: StepKey;
    state: TrackState;
    title: string;
    hint: string;
    at: Date | null;
}

export interface Track {
    rows: TrackRow[];
    phase: TrackPhase;
    /** 0..1: how much of the line between the first and last node is drawn. */
    progress: number;
    /** Index of the row being worked on, or -1. */
    activeIndex: number;
    headline: string;
}

function plural(n: number, one: string, many: string): string {
    return `${n.toLocaleString()} ${n === 1 ? one : many}`;
}

function domainPhrase(names: string[]): string {
    if (names.length === 0) return "your domain";
    if (names.length === 1) return names[0];
    return plural(names.length, "domain", "domains");
}

interface Copy {
    pending: string;
    active: string;
    done: string;
    hint: string;
}

function copyFor(key: StepKey, names: string[], mailboxes: number): Copy {
    const d = domainPhrase(names);
    const m = plural(mailboxes, "mailbox", "mailboxes");
    switch (key) {
        case "register":
            return {
                pending: `Register ${d}`,
                active: `Registering ${d}`,
                done: `Registered ${d}`,
                hint: "Yours for a year, with WHOIS privacy where the extension allows it.",
            };
        case "dns":
            return {
                pending: "Set up DNS",
                active: "Pointing DNS at the mail servers",
                done: "DNS is set up",
                hint: "MX records and the website forward, hosted and kept up for you.",
            };
        case "mail_auth":
            return {
                pending: "Publish SPF, DKIM and DMARC",
                active: "Publishing SPF, DKIM and DMARC",
                done: "SPF, DKIM and DMARC published",
                hint: "So receiving servers can tell your mail is really from you.",
            };
        case "mailboxes":
            return {
                pending: `Create ${m}`,
                active: `Creating ${m}`,
                done: `${m.charAt(0).toUpperCase()}${m.slice(1)} created`,
                hint: "Real Google Workspace or Microsoft 365 accounts, one per address.",
            };
        case "delegation":
            return {
                pending: "Connect your mailboxes to Warmbly",
                active: "Connecting your mailboxes to Warmbly",
                done: "Mailboxes connected to Warmbly",
                hint: "No passwords to copy and no sign-in prompts.",
            };
        case "ready":
            return {
                pending: "Start warming up",
                active: "Starting warmup",
                done: "Warming up",
                hint: "Warmup starts slowly and ramps on its own.",
            };
    }
}

function toState(s: StepStatus | undefined): TrackState {
    switch (s) {
        case "running":
            return "active";
        case "done":
        case "failed":
        case "skipped":
            return s;
        default:
            return "pending";
    }
}

function toDate(v: OrderStep["at"]): Date | null {
    if (!v) return null;
    const d = v instanceof Date ? v : new Date(v);
    return Number.isNaN(d.getTime()) ? null : d;
}

/** The track for an order; `fallback` fills names and counts before the order lists them. */
export function deriveTrack(order: Pick<Order, "status" | "steps" | "domains" | "mailboxes"> | null | undefined, fallback?: { domains?: string[]; mailboxes?: number }): Track {
    const byKey = new Map((order?.steps ?? []).map((s) => [s.key, s]));
    const names = order?.domains?.length ? order.domains.map((d) => d.name) : (fallback?.domains ?? []);
    const count = order?.mailboxes?.length ? order.mailboxes.length : (fallback?.mailboxes ?? 0);
    const status = order?.status ?? "charging";

    const rows: TrackRow[] = STEP_KEYS.map((key) => {
        const step = byKey.get(key);
        let state = toState(step?.status);
        // A finished order has no step left running, whatever the last snapshot said.
        if (status === "completed" && (state === "pending" || state === "active")) state = "done";
        const c = copyFor(key, names, count);
        const title = state === "done" || state === "skipped" ? c.done : state === "active" ? c.active : c.pending;
        return { key, state, title, hint: c.hint, at: toDate(step?.at) };
    });

    // While processing, the first unfinished step is the one under way even before the server marks it running.
    let activeIndex = rows.findIndex((r) => r.state === "active");
    if (activeIndex < 0 && status === "processing") {
        activeIndex = rows.findIndex((r) => r.state === "pending");
        if (activeIndex >= 0) {
            const r = rows[activeIndex];
            rows[activeIndex] = { ...r, state: "active", title: copyFor(r.key, names, count).active };
        }
    }

    const finished = rows.filter((r) => r.state === "done" || r.state === "skipped").length;
    // Count the leading run of finished rows, plus half a segment toward the one under way.
    let lead = 0;
    while (lead < rows.length && (rows[lead].state === "done" || rows[lead].state === "skipped")) lead++;
    const segments = rows.length - 1;
    const drawn = lead === 0 ? 0 : Math.min(segments, lead - 1 + (activeIndex === lead ? 0.5 : 0));
    const progress = status === "completed" ? 1 : segments > 0 ? drawn / segments : 0;

    const failed = rows.some((r) => r.state === "failed");
    const phase: TrackPhase =
        status === "charging"
            ? "paying"
            : status === "completed"
              ? "done"
              : status === "partially_failed"
                ? "partial"
                : status === "failed"
                  ? "failed"
                  : failed
                    ? "partial"
                    : "working";

    const headline =
        phase === "paying"
            ? "Confirming your payment"
            : phase === "done"
              ? count > 0
                  ? `${plural(count, "mailbox is", "mailboxes are")} ready`
                  : "Everything is ready"
              : phase === "partial"
                ? "Most of it is ready"
                : phase === "failed"
                  ? "This order could not be completed"
                  : activeIndex >= 0
                    ? `${rows[activeIndex].title}…`
                    : finished === rows.length
                      ? "Finishing up…"
                      : "Getting started…";

    return { rows, phase, progress, activeIndex, headline };
}

/** Order re-reads while the socket is down: fast enough that the track moves as the work does. */
export const ORDER_POLL_OFFLINE_MS = 2_000;
/** A safety net under realtime while the socket is live. */
export const ORDER_POLL_LIVE_MS = 30_000;

/** How often an order view re-reads itself: only while something is still moving, and fast when realtime is down. */
export function orderPollMs(order: Pick<Order, "status" | "mailboxes"> | undefined, socketLive: boolean): number | false {
    if (!order) return socketLive ? false : ORDER_POLL_OFFLINE_MS;
    const settling = order.mailboxes.some((m) => m.connect_state === "waiting" || m.connect_state === "connecting");
    if (order.status !== "processing" && order.status !== "charging" && !settling) return false;
    return socketLive ? ORDER_POLL_LIVE_MS : ORDER_POLL_OFFLINE_MS;
}

/** How far a step that works item by item has got, for the live counter under it; null for the others. */
export interface StepCount {
    done: number;
    total: number;
    noun: string;
}

const DOMAIN_WAITING = new Set(["registering", "connecting", "failed"]);

export function stepCount(order: Pick<Order, "domains" | "mailboxes"> | undefined, key: StepKey): StepCount | null {
    if (!order) return null;
    const domains = order.domains ?? [];
    const boxes = order.mailboxes ?? [];
    switch (key) {
        case "register":
            return domains.length > 1 ? { done: domains.filter((d) => !DOMAIN_WAITING.has(d.status)).length, total: domains.length, noun: "registered" } : null;
        case "mailboxes":
            return boxes.length > 1 ? { done: boxes.filter((m) => m.status !== "provisioning" && m.status !== "failed").length, total: boxes.length, noun: "created" } : null;
        case "delegation":
            return boxes.length > 1 ? { done: boxes.filter((m) => m.connect_state === "connected").length, total: boxes.length, noun: "connected" } : null;
        default:
            return null;
    }
}

/** The one line under the headline: what is happening right now, in plain words. */
export function activityLine(track: Track, names: string[]): string {
    if (track.phase === "paying") return "Taking the payment. Setup starts the moment it lands.";
    if (track.phase === "partial" || track.phase === "failed") return "Anything that could not be set up was refunded to your balance.";
    const row = track.activeIndex >= 0 ? track.rows[track.activeIndex] : null;
    const d = names.length === 1 ? names[0] : "each domain";
    switch (row?.key) {
        case "register":
            return names.length > 1 ? "Asking the registries for each domain, with WHOIS privacy where the extension allows it." : `Asking the registry for ${d}, with WHOIS privacy where the extension allows it.`;
        case "dns":
            return `Pointing ${d} at the mail servers and setting up the website forward.`;
        case "mail_auth":
            return "Publishing the records receiving servers check before they trust your mail.";
        case "mailboxes":
            return "The provider is creating the mailboxes. This can take a while; nothing is needed from you.";
        case "delegation":
            return "Connecting each mailbox to Warmbly. No passwords to copy and no sign-in prompts.";
        case "ready":
            return "Turning warmup on. It starts slowly and ramps on its own.";
        default:
            return "You can leave this page. It carries on, and you get a notification when it is done.";
    }
}

/** Steps finished, for the "x of 6" counter. */
export function stepsDone(track: Track): number {
    return track.rows.filter((r) => r.state === "done" || r.state === "skipped").length;
}
