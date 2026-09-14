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
    capture: (error: unknown) => void;
    identify: (identity: Identity) => void;
    step: (message: string, properties?: StepProperties) => void;
};

const backends: Backend[] = [];

// EARLY_LIMIT bounds the pre-load buffers: a render loop that throws every
// frame must not grow them without end.
const EARLY_LIMIT = 20;
let early: unknown[] = [];
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
                          capture: (error) => void posthog.captureException(error),
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
                    sendDefaultPii: true,
                    environment: SENTRY_ENVIRONMENT,
                    // Empty is omitted rather than sent: an event tagged with
                    // the empty release matches no uploaded source map and
                    // reads as a real release.
                    release: SENTRY_RELEASE || undefined,
                });
                settle({
                    capture: (error) => void Sentry.captureException(error),
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
                capture_dead_clicks: true,
                capture_heatmaps: true,
                rageclick: true,
                capture_performance: { web_vitals: true, network_timing: true },
                disable_session_recording: !POSTHOG_SESSION_REPLAY,
                session_recording: {
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
export function captureException(error: unknown): void {
    // Remembered as well as reported while a backend is still loading, so the
    // one that has not arrived yet gets it on replay. Only the newly settled
    // backend replays, so nothing is reported twice.
    if (awaiting > 0) remember(error);
    for (const backend of backends) backend.capture(error);
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
        for (const error of early) backend.capture(error);
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

function remember(error: unknown): void {
    if (early.length >= EARLY_LIMIT) return;
    early.push(error);
}
