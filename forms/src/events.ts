// Funnel beacons: view on load, start on first interaction, page as the
// visitor advances. Fire-and-forget; analytics must never slow the form or
// surface an error. Submits are recorded server-side by the submit pipeline.

import { apiHeaders, personalToken } from "./api";
import { track } from "./observability";

const VISITOR_KEY_STORAGE = "wf_vk";

let memoryKey: string | null = null;

// visitorKey is a random, non-identifying id that lets analytics tell "one
// visitor saw four pages" from "four visitors". Storage can be blocked in
// third-party iframes; fall back gracefully to a per-load key.
export function visitorKey(): string {
    if (memoryKey) return memoryKey;
    const mint = () => crypto.randomUUID();
    try {
        let k = localStorage.getItem(VISITOR_KEY_STORAGE);
        if (!k) {
            k = mint();
            localStorage.setItem(VISITOR_KEY_STORAGE, k);
        }
        memoryKey = k;
    } catch {
        memoryKey = mint();
    }
    return memoryKey;
}

interface EventPayload {
    type: "view" | "start" | "page";
    page_index: number;
    pages_total: number;
}

function sendEvent(publicId: string, e: EventPayload) {
    // keepalive lets a beacon survive navigation; sendBeacon cannot carry the
    // render-token header, so plain fetch it is.
    void fetch(`/api/forms/${encodeURIComponent(publicId)}/events`, {
        method: "POST",
        keepalive: true,
        headers: { "Content-Type": "application/json", ...apiHeaders() },
        body: JSON.stringify({
            ...e,
            visitor_key: visitorKey(),
            source_url: document.referrer || window.location.href,
            link_token: personalToken() ?? undefined,
        }),
    }).catch(() => undefined);
}

export interface Tracker {
    view(): void;
    start(): void;
    page(index: number): void;
}

// makeTracker enforces the once-guards client-side (the server dedupes views
// again by visitor key): one view per mount, one start, and page events only
// when the visitor reaches a page they have not seen this session.
export function makeTracker(publicId: string, pagesTotal: number): Tracker {
    let viewed = false;
    let started = false;
    let maxPage = -1;
    return {
        view() {
            if (viewed) return;
            viewed = true;
            track("form_viewed", publicId);
            sendEvent(publicId, { type: "view", page_index: 0, pages_total: pagesTotal });
        },
        start() {
            if (started) return;
            started = true;
            track("form_started", publicId);
            sendEvent(publicId, { type: "start", page_index: Math.max(maxPage, 0), pages_total: pagesTotal });
        },
        page(index: number) {
            if (index <= maxPage) return;
            maxPage = index;
            sendEvent(publicId, { type: "page", page_index: index, pages_total: pagesTotal });
        },
    };
}
