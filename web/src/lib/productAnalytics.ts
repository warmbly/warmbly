// Named product events.
//
// The client itself lives in lib/posthog, which product analytics, session
// replay and error tracking share. Autocapture already records every click and
// pageview; this file is the closed list of the moments worth a name of their
// own, so a funnel can be built on them without guessing at element text.
import { loadPostHog, postHogClient } from "./posthog";
import { POSTHOG_KEY } from "./information";

// initProductAnalytics loads and configures the SDK, once, and only when a key
// is configured. Loaded as its own chunk so an install with no key pays neither
// the bytes nor a request.
export function initProductAnalytics(): void {
    void loadPostHog();
}

// Event is the closed set of product events the dashboard reports. Keeping it a
// union rather than a string means a typo is a build error and the list stays
// readable as the answer to "what do we actually measure".
export type Event =
    | "mailbox_connected"
    | "campaign_launched";

// capture reports one product event. A no-op when analytics is off. The
// signed-in person and workspace are already on the event through identify,
// so properties are for what happened: a provider name, a step count.
export function capture(event: Event, properties?: Record<string, string | number | boolean>): void {
    if (!POSTHOG_KEY) return;
    // The SDK may still be in flight on a fast first action; dropping the event
    // is better than queueing one that arrives without its session.
    postHogClient()?.capture(event, properties);
}
