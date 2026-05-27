import { BackendsPage } from "./BackendsPage";

export default function CachePage() {
    return (
        <BackendsPage
            kind="cache"
            title="Cache"
            description="Redis-compatible cache used for rate-limit counters, plaintext DEK caching, session bookkeeping, and short-lived worker-side dedupe."
            notes="Worker-local state is intentionally minimal and disposable. The cache backs the few latency-sensitive paths workers do touch directly."
        />
    );
}
