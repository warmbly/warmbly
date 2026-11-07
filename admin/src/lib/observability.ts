// Browser analytics, session replay and error reporting for the operator panel.
//
// Same rule as the dashboard: PostHog is the default backend and Sentry is
// still supported, both come from the container-injected runtime config, and an
// install that configures neither reports nothing anywhere. The admin image
// ships to self-hosters too, so a literal key or DSN here would make every
// self-hosted panel report its operator's errors and URLs to somebody else.
//
// Each SDK loads as its own chunk and only when it is configured, so an install
// with neither fetches nothing. The two listeners below cover the window while
// a chunk is in flight, which is exactly when a broken deploy throws, and come
// off once every configured backend has installed its own.
//
// On PostHog the panel is measured the way the dashboard is: pageviews,
// autocapture, heatmaps, web vitals and session replay, with the operator
// identified by their account, so an issue is answerable and a slow screen is
// findable. The panel loads PostHog as a plain import rather than through a
// shared client module because it has no product events of its own to name.
import type { CaptureResult } from "posthog-js";
import {
    POSTHOG_ERROR_TRACKING,
    POSTHOG_HOST,
    POSTHOG_KEY,
    POSTHOG_SESSION_REPLAY,
    POSTHOG_UI_HOST,
    SENTRY_DSN,
    SENTRY_ENVIRONMENT,
    SENTRY_RELEASE,
} from "./env";

export type Identity = { userId: string; email?: string | null; name?: string | null } | null;
export type StepProperties = Record<string, string | number | boolean>;

type Backend = {
    capture: (error: unknown, properties?: StepProperties) => void;
    identify: (identity: Identity) => void;
    step: (message: string, properties?: StepProperties) => void;
};

const backends: Backend[] = [];

// EARLY_LIMIT bounds the pre-load buffers: a render loop that throws every
// frame must not grow them without end.
const EARLY_LIMIT = 20;
let early: Array<{ error: unknown; properties?: StepProperties }> = [];
let earlySteps: Array<{ message: string; properties?: StepProperties }> = [];

// identity is remembered rather than forwarded once, because a backend that
// finishes loading after sign-in still has to learn who is signed in.
let identity: Identity = null;

// awaiting counts the backends still loading. At zero the buffers are done.
let awaiting = 0;
let removeEarlyHandlers: (() => void) | null = null;

// initErrorReporting is called once, before the app renders. The name is
// historical: on PostHog it is also what starts analytics and session replay,
// which are the same SDK and the same key.
export function initErrorReporting(): void {
    const postHog = Boolean(POSTHOG_KEY);
    const sentry = Boolean(SENTRY_DSN);
    if (!(postHog && POSTHOG_ERROR_TRACKING) && !sentry) {
        // No exception backend: analytics alone still loads below, but there
        // is nothing to buffer errors for.
        if (postHog) void loadPostHog();
        return;
    }

    installEarlyHandlers();

    if (postHog) {
        awaiting++;
        void loadPostHog()
            .then((posthog) =>
                settle(posthog && POSTHOG_ERROR_TRACKING
                    ? {
                          capture: (error, properties) => void posthog.captureException(error, properties),
                          identify: (next) => identifyPostHog(posthog, next),
                          step: (message, properties) => posthog.addExceptionStep(message, properties),
                      }
                    : null),
            );
    }

    if (sentry) {
        awaiting++;
        void import("@sentry/react")
            .then((Sentry) => {
                Sentry.init({
                    dsn: SENTRY_DSN,
                    // Off deliberately. The user id, email and name are
                    // attached below through setUser, which is the identity an
                    // exception needs. sendDefaultPii adds request headers,
                    // cookies and bodies on top of that, and an Authorization
                    // header in a crash report is a session handed to whoever
                    // can read the project.
                    sendDefaultPii: false,
                    environment: SENTRY_ENVIRONMENT,
                    // Empty is omitted rather than sent: an event tagged with
                    // the empty release matches no uploaded source map and
                    // reads as a real release.
                    release: SENTRY_RELEASE || undefined,
                });
                settle({
                    capture: (error, properties) =>
                        void Sentry.captureException(error, properties ? { extra: properties } : undefined),
                    identify: (next) =>
                        Sentry.setUser(next
                            ? { id: next.userId, email: next.email ?? undefined, username: next.name ?? undefined }
                            : null),
                    step: (message, properties) =>
                        Sentry.addBreadcrumb({ category: "app", message, data: properties, level: "info" }),
                });
            })
            .catch(() => settle(null));
    }
}

// loadPostHog initialises the SDK once and resolves it, or null when the chunk
// could not be fetched. Analytics and replay start here whether or not error
// tracking is on; the exception handlers are the one part gated separately.
let postHogLoading: Promise<import("posthog-js").PostHog | null> | null = null;
function loadPostHog(): Promise<import("posthog-js").PostHog | null> {
    if (postHogLoading) return postHogLoading;
    postHogLoading = import("posthog-js")
        .then(({ posthog }) => {
            posthog.init(POSTHOG_KEY, {
                api_host: POSTHOG_HOST,
                ui_host: POSTHOG_UI_HOST,
                person_profiles: "identified_only",
                autocapture: true,
                // A single-page app: one load, then history changes.
                capture_pageview: "history_change",
                capture_pageleave: true,
                // Same URL masking as the dashboard: tokens and codes can travel in a query string.
                mask_personal_data_properties: true,
                custom_personal_data_properties: ["token", "session", "agent_session", "code", "invite", "next"],
                capture_dead_clicks: true,
                capture_heatmaps: true,
                rageclick: true,
                capture_performance: { web_vitals: true, network_timing: true },
                disable_session_recording: !POSTHOG_SESSION_REPLAY,
                // Same masking as the dashboard: data-ph-mask hides a revealed
                // secret's text and blocks an input holding one.
                session_recording: {
                    maskAllInputs: false,
                    maskInputOptions: { password: true },
                    maskTextSelector: "[data-ph-mask], .ph-mask",
                    blockSelector: "input[data-ph-mask], textarea[data-ph-mask], [data-ph-mask] input, [data-ph-mask] textarea, [data-input-otp], input[autocomplete='one-time-code']",
                },
                // Replayed console output is retained; exceptions are captured separately.
                enable_recording_console_log: false,
                respect_dnt: false,
                capture_exceptions: POSTHOG_ERROR_TRACKING
                    ? {
                          capture_unhandled_errors: true,
                          capture_unhandled_rejections: true,
                          capture_console_errors: true,
                      }
                    : false,
                before_send: dropBrowserNoise,
            });
            // Registered rather than passed per call so an autocaptured
            // event carries them too. Named so panel events are separable
            // from the dashboard's in a shared project, the same way the Go
            // services set a service property.
            posthog.register(SENTRY_RELEASE
                ? { service: "admin", environment: SENTRY_ENVIRONMENT, release: SENTRY_RELEASE }
                : { service: "admin", environment: SENTRY_ENVIRONMENT });
            if (identity) identifyPostHog(posthog, identity);
            return posthog;
        })
        .catch(() => null);
    return postHogLoading;
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

// Expected states, not faults: a lapsed session signs the operator out, and a closed confirm prompt is a choice.
const NOISE_TYPES = ["SessionExpiredError", "ReauthCancelledError"];

// Thrown into the page by wallets, extensions and in-app browsers; none of these names exist in our code.
const FOREIGN_MESSAGES = [
    /window\.ethereum\.selectedAddress = undefined/,
    /^Can't find variable: (CONFIG|currentInset)$/,
    /^No Listener: tabs:/,
    /^Could not establish connection\. Receiving end does not exist\.$/,
];
const EXTENSION_SOURCE = /^(?:(?:chrome|moz|safari|safari-web|ms-browser)-extension:|webkit-masked-url:)/;
// Our bundle is served from /assets/; a frame there means the error passed through our code.
const BUNDLE_SOURCE = /^(?:[a-z]+:\/\/[^/]+)?\/assets\//i;

// isForeign keeps anything with a frame in our bundle, so a real error with the same words still reports.
function isForeign(entry: Record<string, unknown>): boolean {
    const trace = entry.stacktrace as { frames?: unknown } | undefined;
    const sources = (Array.isArray(trace?.frames) ? trace.frames : []).flatMap((frame) => {
        const f = frame as { filename?: unknown; abs_path?: unknown } | null;
        const source = f?.filename ?? f?.abs_path;
        return typeof source === "string" ? [source] : [];
    });
    if (sources.some((source) => BUNDLE_SOURCE.test(source))) return false;
    if (sources.some((source) => EXTENSION_SOURCE.test(source))) return true;
    const value = entry.value ?? entry.$exception_value;
    return typeof value === "string" && FOREIGN_MESSAGES.some((pattern) => pattern.test(value));
}

export function dropBrowserNoise(event: CaptureResult | null): CaptureResult | null {
    if (event?.event !== "$exception") return event;
    const exceptionList = event.properties?.$exception_list;
    const entries = (Array.isArray(exceptionList) ? exceptionList : [])
        .filter((exception): exception is Record<string, unknown> => Boolean(exception) && typeof exception === "object");
    if (entries.length > 0 && entries.every(isForeign)) return null;
    const structuredNoise = entries.some((entry) => {
        const value = entry.value ?? entry.$exception_value;
        const type = entry.type ?? entry.$exception_type;
        return (typeof value === "string" && NOISE.includes(value.trim()))
            || (typeof type === "string" && NOISE_TYPES.includes(type));
    });
    const message = event.properties?.$exception_message;
    const messageNoise = typeof message === "string" && NOISE.includes(message.trim());
    // Keep accepting flattened payloads while cached SDK chunks are still in
    // browsers during a rolling release.
    const values = event.properties?.$exception_values;
    const legacyNoise = Array.isArray(values)
        && values.some((value) => typeof value === "string" && NOISE.includes(value.trim()));
    const noise = structuredNoise || messageNoise || legacyNoise;
    return noise ? null : event;
}

// identifyPostHog names the operator, or resets the device on sign-out so the
// next person at the same browser is not attributed to the last one.
function identifyPostHog(posthog: import("posthog-js").PostHog, next: Identity): void {
    if (!next) {
        posthog.reset();
        return;
    }
    const person: Record<string, string> = {};
    if (next.email) person.email = next.email;
    if (next.name) person.name = next.name;
    posthog.identify(next.userId, person);
}

// captureException reports an error the app handled itself. A no-op when no
// backend is configured.
export function captureException(error: unknown, properties?: StepProperties): void {
    // Remembered as well as reported while a backend is still loading, so the
    // one that has not arrived yet gets it on replay. Only the newly settled
    // backend replays, so nothing is reported twice.
    if (awaiting > 0) remember(error, properties);
    for (const backend of backends) backend.capture(error, properties);
}

// setErrorIdentity names the operator later events belong to. Pass null on
// sign-out. It reaches PostHog even when error tracking is off, because the
// identify is what analytics and replay hang off.
export function setErrorIdentity(next: Identity): void {
    identity = next;
    for (const backend of backends) backend.identify(next);
    if (!POSTHOG_ERROR_TRACKING && POSTHOG_KEY) {
        void loadPostHog().then((posthog) => posthog && identifyPostHog(posthog, identity));
    }
}

// noteStep adds one step to the trail the next exception carries. Keep the
// message short.
export function noteStep(message: string, properties?: StepProperties): void {
    if (awaiting > 0 && earlySteps.length < EARLY_LIMIT) earlySteps.push({ message, properties });
    for (const backend of backends) backend.step(message, properties);
}

function settle(backend: Backend | null): void {
    if (backend) {
        backends.push(backend);
        if (identity) backend.identify(identity);
        for (const step of earlySteps) backend.step(step.message, step.properties);
        for (const entry of early) backend.capture(entry.error, entry.properties);
    }

    awaiting--;
    if (awaiting > 0) return;

    // Every configured backend has its own global handlers installed by now, so
    // keeping ours would report the next unhandled error twice.
    removeEarlyHandlers?.();
    removeEarlyHandlers = null;
    early = [];
    earlySteps = [];
}

function installEarlyHandlers(): void {
    if (removeEarlyHandlers || typeof window === "undefined") return;

    const onError = (event: ErrorEvent) => remember(event.error ?? event.message);
    const onRejection = (event: PromiseRejectionEvent) => remember(event.reason);
    window.addEventListener("error", onError);
    window.addEventListener("unhandledrejection", onRejection);

    removeEarlyHandlers = () => {
        window.removeEventListener("error", onError);
        window.removeEventListener("unhandledrejection", onRejection);
    };
}

function remember(error: unknown, properties?: StepProperties): void {
    if (early.length >= EARLY_LIMIT) return;
    early.push({ error, properties });
}
