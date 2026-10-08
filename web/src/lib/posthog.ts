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
// heatmaps, rage and dead clicks, web vitals, network timing, session replay,
// surveys and exceptions. Replay masks password fields, one-time codes and any
// secret the page reveals (data-ph-mask). The privacy page on the marketing
// site describes exactly this and has to change with it.
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
                // Sign-in sessions, invitation and setup tokens and pairing codes travel in the URL.
                mask_personal_data_properties: true,
                custom_personal_data_properties: ["token", "session", "agent_session", "code", "invite", "next"],
                capture_dead_clicks: true,
                capture_heatmaps: true,
                rageclick: true,
                capture_performance: { web_vitals: true, network_timing: true },
                disable_session_recording: !POSTHOG_SESSION_REPLAY,
                session_recording: {
                    // Password inputs cover the mailbox app password and the
                    // API secret. They do not cover the one-time codes, which
                    // are plain inputs so the browser will autofill them from
                    // SMS and mail, nor a secret the page has already revealed
                    // as text. Those carry data-ph-mask / .ph-mask: text is
                    // masked, and an input is blocked, since text masking
                    // never reaches an input's value.
                    maskAllInputs: false,
                    maskInputOptions: { password: true },
                    maskTextSelector: "[data-ph-mask], .ph-mask",
                    blockSelector: "input[data-ph-mask], textarea[data-ph-mask], [data-ph-mask] input, [data-ph-mask] textarea, [data-input-otp], input[autocomplete='one-time-code']",
                },
                // Console output is replayed alongside the session, so anything
                // printed is retained. Off: it is not worth one careless log of
                // a token, and exceptions are captured separately below.
                enable_recording_console_log: false,
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

// Browser noise: reported by the window error handler, carrying no stack we
// can act on and no bug behind it. "Script error." is what a cross-origin
// script is flattened to, and the ResizeObserver notice is a benign scheduling
// message the spec requires browsers to fire. Both drown the real issues.
const NOISE = [
    "Script error.",
    "ResizeObserver loop completed with undelivered notifications.",
    "ResizeObserver loop limit exceeded",
];

// A session ending is a lifecycle event, not a crash: normalizeError turns an
// AuthError into a redirect and UserProvider sends the user to sign in. It
// reached error tracking only by also escaping to the global rejection handler.
// PasskeyCancelled is control flow by definition: the sign-in page aborts with it when it is left.
const NOISE_TYPES = ["AuthError", "PasskeyCancelled"];

// posthog-js aborts its own slow requests with this named AbortError and retries them; an app abort stays reported.
function isSdkRequestTimeout(type: unknown, value: unknown): boolean {
    return type === "AbortError" && typeof value === "string" && value.startsWith("PostHog request timed out");
}

// Keep the known raw refusal quiet too, without hiding unrelated InvalidStateErrors.
function isPasskeyAlreadyRegistered(type: unknown, value: unknown): boolean {
    if (type === "PasskeyAlreadyRegistered") return true;
    if (type !== "DOMException" && type !== "InvalidStateError") return false;
    return value === "The authenticator was previously registered"
        || value === "The authenticator was previously registered: InvalidStateError"
        || value === "InvalidStateError: The authenticator was previously registered";
}

// An exception whose message is an object's default toString, with no stack and
// no real Error behind it, carries nothing: no name, no cause, no place. They
// arrive from extensions and from handlers that concatenate a DOM Event into a
// string, and they all fingerprint together.
//
// All three conditions are required. `synthetic` marks a value PostHog wrapped
// because it was thrown as something other than an Error, so anything we
// construct ourselves is excluded even when its message also stringified an
// object; and a single frame would make it findable, so a stack of any depth is
// kept.
function isUninformative(entry: Record<string, unknown>): boolean {
    const value = entry.value ?? entry.$exception_value;
    if (typeof value !== "string" || !DEFAULT_OBJECT_STRING.test(value)) return false;
    const mechanism = entry.mechanism as { synthetic?: unknown } | undefined;
    if (mechanism?.synthetic !== true) return false;
    const trace = entry.stacktrace as { frames?: unknown[] } | undefined;
    return !Array.isArray(trace?.frames) || trace.frames.length === 0;
}

const DEFAULT_OBJECT_STRING = /\[object [A-Z][A-Za-z]*\]/;

export function isNoise(properties: Properties): boolean {
    const exceptionList = properties.$exception_list;
    if (Array.isArray(exceptionList) && exceptionList.length > 0 && exceptionList.every((exception) => {
        if (!exception || typeof exception !== "object") return false;
        const entry = exception as Record<string, unknown>;
        return isPasskeyAlreadyRegistered(entry.type ?? entry.$exception_type, entry.value ?? entry.$exception_value);
    })) return true;
    if (Array.isArray(exceptionList) && exceptionList.some((exception) => {
        if (!exception || typeof exception !== "object") return false;
        const entry = exception as Record<string, unknown>;
        const type = entry.type ?? entry.$exception_type;
        const value = entry.value ?? entry.$exception_value;
        return (typeof type === "string" && NOISE_TYPES.includes(type))
            || (typeof value === "string" && NOISE.includes(value.trim()))
            || isSdkRequestTimeout(type, value)
            || isUninformative(entry);
    })) {
        return true;
    }

    const type = properties.$exception_type;
    if (typeof type === "string" && NOISE_TYPES.includes(type)) return true;
    const message = properties.$exception_message;
    if (typeof message === "string" && NOISE.includes(message.trim())) return true;
    if (isSdkRequestTimeout(type, message)) return true;
    const hasExceptionList = Array.isArray(exceptionList) && exceptionList.length > 0;
    if (!hasExceptionList && isPasskeyAlreadyRegistered(type, message)) return true;

    // Keep accepting flattened payloads while cached SDK chunks are still in
    // browsers during a rolling release.
    const types = properties.$exception_types;
    if (Array.isArray(types) && types.some((t) => typeof t === "string" && NOISE_TYPES.includes(t))) return true;
    const values = properties.$exception_values;
    if (!Array.isArray(values)) return false;
    if (Array.isArray(types) && types.some((t, i) => isSdkRequestTimeout(t, values[i]))) return true;
    if (!hasExceptionList && Array.isArray(types) && types.length > 0 && types.length === values.length
        && types.every((t, i) => isPasskeyAlreadyRegistered(t, values[i]))) return true;
    return values.some((v) => typeof v === "string" && NOISE.includes(v.trim()));
}

// decorate is the last thing to touch an event before it is sent. Exceptions
// get the account and workspace ids as flat properties on top of the person,
// so "every error this workspace hit" is a property filter.
function decorate(event: CaptureResult | null): CaptureResult | null {
    if (!event?.properties || event.event !== "$exception") return event;
    if (isNoise(event.properties)) return null;
    if (!identity) return event;
    event.properties.user_id = identity.userId;
    if (identity.organizationId) event.properties.organization_id = identity.organizationId;
    return event;
}
