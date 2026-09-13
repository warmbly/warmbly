// Browser analytics and error reporting for the hosted form page.
//
// PostHog is the default backend and Sentry is still supported; the Go shell
// stamps whichever the operator configured, and with neither, which is every
// self-host and every install that has not configured one, nothing is fetched
// and nothing is sent.
//
// Public pages have to stay light, so unlike the dashboard each SDK loads as
// its own chunk. The cost is that an error thrown in the first few
// milliseconds is missed, which is the right trade on a page whose whole job is
// to render one form for a stranger.
//
// The visitor is a stranger on a customer's form, so PostHog runs cookieless
// here: nothing is stored in their browser and no person is ever created. That
// rules out session replay, which needs a session to exist, and it is the one
// surface where that is the right call: the screen would be somebody typing
// their answers into a customer's form. Pageviews, autocapture, heatmaps, web
// vitals, exceptions and the named funnel events below all work without it.
import type { PostHog } from "posthog-js";

let client: PostHog | null = null;

function meta(name: string): string {
    return document.querySelector<HTMLMetaElement>(`meta[name="${name}"]`)?.content?.trim() ?? "";
}

export function initErrorReporting(): void {
    const release = meta("wf-release") || undefined;
    const environment = meta("wf-environment") || undefined;

    const posthogKey = meta("wf-posthog-key");
    if (posthogKey) {
        const errors = meta("wf-posthog-errors") !== "false";
        void import("posthog-js").then(({ posthog }) => {
            posthog.init(posthogKey, {
                api_host: meta("wf-posthog-host") || "https://us.i.posthog.com",
                cookieless_mode: "always",
                person_profiles: "never",
                autocapture: true,
                capture_pageview: true,
                capture_pageleave: true,
                capture_dead_clicks: true,
                capture_heatmaps: true,
                rageclick: true,
                capture_performance: { web_vitals: true, network_timing: true },
                disable_session_recording: true,
                respect_dnt: false,
                capture_exceptions: errors
                    ? {
                          capture_unhandled_errors: true,
                          capture_unhandled_rejections: true,
                          capture_console_errors: true,
                      }
                    : false,
            });
            // Named so form-page events are separable from the dashboard's in a
            // shared project, the same way the Go services set a service
            // property.
            posthog.register(release
                ? { service: "forms", environment, release }
                : { service: "forms", environment });
            client = posthog;
        }).catch(() => {
            // A blocked or failed SDK load must never stop the form rendering.
        });
    }

    const dsn = meta("wf-sentry-dsn");
    if (dsn) {
        void import("@sentry/browser").then((Sentry) => {
            Sentry.init({
                dsn,
                release,
                environment,
                // Named so form-page errors are separable from the dashboard's
                // in a shared project, the same way the Go services set
                // ServerName.
                initialScope: { tags: { service: "forms" } },
                // A form page carries a stranger's answers. Default PII (their
                // IP, their headers) is not ours to collect, and the
                // dashboard's reasons for sending it do not apply here.
                sendDefaultPii: false,
            });
        }).catch(() => {
            // A blocked or failed SDK load must never stop the form rendering.
        });
    }
}

// Event is the closed set of named form-page events, the funnel a customer's
// form is measured by. The first-party beacons in events.ts feed the
// customer's own numbers; these feed ours.
export type Event = "form_viewed" | "form_started" | "form_submitted";

// track reports one named event. A no-op until the SDK has loaded, and forever
// when no key was stamped. The form's public id is the one property: it names
// the form, never the person filling it in.
export function track(event: Event, form: string): void {
    client?.capture(event, { form });
}
