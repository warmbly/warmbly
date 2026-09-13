// The dashboard's one PostHog client.
//
// Product analytics, session replay and error tracking are the same project
// and the same key, so they are the same SDK instance: two `posthog.init` calls
// would be two visitors, two pageview streams and two sets of global error
// handlers. `lib/productAnalytics` owns what we name, `lib/observability` owns
// what we report; this file owns the client both of them borrow.
//
// It is hosted-only. The dashboard image is the same for the hosted service and
// for a self-host, so the key comes from the container-injected runtime config
// and an unset key means the SDK chunk is never fetched and no PostHog host is
// ever contacted. A self-host therefore ships this code path and never runs it.
//
// It is identified. A signed-in user is `identify`d by their account id, with
// their email and name as person properties, and their workspace is a group,
// so every event, replay and exception is attributable to the account and the
// workspace it happened in. The SDK keeps its device id in localStorage plus a
// cookie, which is what ties the anonymous visit before sign-in to the person
// after it and what makes sessions exist at all: session replay and session
// analytics have no session to hang off in the cookieless mode this used to
// run in.
//
// Everything the SDK can observe is on: autocapture, pageviews and pageleaves,
// heatmaps, rage and dead clicks, web vitals, network timing, session replay
// with console logs, surveys and exceptions. The only thing masked is what a
// password field holds, which is never ours to see. The privacy page on the
// marketing site describes exactly this and has to change with it.
import type { CaptureResult, PostHog, Properties } from "posthog-js";
import {
    POSTHOG_ERROR_TRACKING,
    POSTHOG_HOST,
    POSTHOG_KEY,
    POSTHOG_SESSION_REPLAY,
    POSTHOG_UI_HOST,
    SENTRY_ENVIRONMENT,
    SENTRY_RELEASE,
} from "./information";

export type PostHogIdentity = {
    userId: string;
    email?: string | null;
    name?: string | null;
    organizationId?: string | null;
    organizationName?: string | null;
    plan?: string | null;
};

let client: PostHog | null = null;
let loading: Promise<PostHog | null> | null = null;

// identity is the last one applied, so a client that finishes loading after
// sign-in still learns who is signed in, and so exceptions can carry the ids
// as plain searchable properties as well as through the person.
let identity: PostHogIdentity | null = null;

// loadPostHog resolves the initialised client, or null when no key is
// configured or the chunk could not be fetched. It initialises on the first
// call and every later caller gets the same instance.
export function loadPostHog(): Promise<PostHog | null> {
    if (!POSTHOG_KEY) return Promise.resolve(null);
    if (loading) return loading;

    loading = import("posthog-js")
        .then(({ posthog }) => {
            posthog.init(POSTHOG_KEY, {
                api_host: POSTHOG_HOST,
                ui_host: POSTHOG_UI_HOST,
                // A person profile is created on identify and not before, so
                // a visitor who never signs in is a device, not a person.
                person_profiles: "identified_only",
                autocapture: true,
                // The dashboard is a single-page app, so page loads happen
                // once and every navigation after that is a history change.
                // Plain `true` would report one pageview per session.
                capture_pageview: "history_change",
                capture_pageleave: true,
                capture_dead_clicks: true,
                capture_heatmaps: true,
                rageclick: true,
                capture_performance: { web_vitals: true, network_timing: true },
                disable_session_recording: !POSTHOG_SESSION_REPLAY,
                session_recording: {
                    // Only what is typed into a password field is hidden. A
                    // mailbox's app password and an API secret are both
                    // password inputs, so that is exactly the credential set.
                    maskAllInputs: false,
                    maskInputOptions: { password: true },
                },
                enable_recording_console_log: true,
                respect_dnt: false,
                capture_exceptions: POSTHOG_ERROR_TRACKING
                    ? {
                          capture_unhandled_errors: true,
                          capture_unhandled_rejections: true,
                          capture_console_errors: true,
                      }
                    : false,
                before_send: decorate,
            });
            // Registered rather than passed per call so an autocaptured
            // event carries them too, and so dashboard events are separable
            // from the admin panel's in a shared project.
            posthog.register(SENTRY_RELEASE
                ? { service: "dashboard", environment: SENTRY_ENVIRONMENT, release: SENTRY_RELEASE }
                : { service: "dashboard", environment: SENTRY_ENVIRONMENT });
            client = posthog;
            if (identity) applyIdentity(posthog, identity);
            return posthog;
        })
        .catch(() => {
            // Blocked or failed: reporting is never a reason the dashboard breaks.
            return null;
        });

    return loading;
}

// postHogClient is the loaded client, or null while it is still in flight. Use
// it where dropping the call is better than waiting for one.
export function postHogClient(): PostHog | null {
    return client;
}

// setPostHogIdentity names the signed-in user and their workspace. Null on
// sign-out resets the SDK, so a shared machine's next session gets a fresh
// device id and is not attributed to whoever used it last.
export function setPostHogIdentity(next: PostHogIdentity | null): void {
    identity = next;
    if (!client) return;
    if (next) {
        applyIdentity(client, next);
    } else {
        client.reset();
    }
}

function applyIdentity(posthog: PostHog, next: PostHogIdentity): void {
    const person: Properties = {};
    if (next.email) person.email = next.email;
    if (next.name) person.name = next.name;
    posthog.identify(next.userId, person);
    if (next.organizationId) {
        const group: Properties = {};
        if (next.organizationName) group.name = next.organizationName;
        if (next.plan) group.plan = next.plan;
        posthog.group("organization", next.organizationId, group);
    }
}

// notePostHogStep records one step on the trail attached to the next exception.
// PostHog buffers them itself, oldest evicted past its byte budget.
//
// Steps recorded before the SDK loaded are held by lib/observability, which
// replays them the moment this client settles, so nothing buffers twice.
export function notePostHogStep(message: string, properties?: Properties): void {
    client?.addExceptionStep(message, properties);
}

// decorate is the last thing to touch an event before it is sent. Exceptions
// get the account and workspace ids as flat properties on top of the person,
// so "every error this workspace hit" is a property filter.
function decorate(event: CaptureResult | null): CaptureResult | null {
    if (!event?.properties || event.event !== "$exception" || !identity) return event;
    event.properties.user_id = identity.userId;
    if (identity.organizationId) event.properties.organization_id = identity.organizationId;
    return event;
}
