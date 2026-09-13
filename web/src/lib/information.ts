import { runtimeEnv } from "./runtimeConfig";

// These read the container-injected runtime config first (so one built image
// works for any deployment), then the value Vite baked at build time.
export const APP_URL = runtimeEnv("APP_URL", import.meta.env.VITE_APP_URL);
export const API_URL = runtimeEnv("API_URL", import.meta.env.VITE_API_URL);
// The whole dashboard talks to the versioned API. VITE_API_URL is a bare origin
// (no path), so this is the single place the /v1 prefix is applied.
export const API_BASE_URL = `${API_URL}/v1`;
export const TURNSTILE_KEY = runtimeEnv("TURNSTILE_KEY", import.meta.env.VITE_TURNSTILE_KEY);

// Shown once in a dialog and then as a header pill. Empty means this is not a
// preview deployment and nothing renders, which is what production wants
// without anyone having to remember to unset it.
export const BETA_NOTICE = runtimeEnv("BETA_NOTICE", import.meta.env.VITE_BETA_NOTICE);
// Empty means the Sentry backend is never initialised. PostHog is the default
// one; either, both or neither can be configured. See lib/observability.
export const SENTRY_DSN = runtimeEnv("SENTRY_DSN", import.meta.env.VITE_SENTRY_DSN);
// The deployment label events are tagged with, whichever backend receives them.
// Runtime, so one image can serve staging and production. The name stays
// SENTRY_* because it is the container variable deployments already set.
export const SENTRY_ENVIRONMENT = runtimeEnv("SENTRY_ENVIRONMENT", import.meta.env.VITE_SENTRY_ENVIRONMENT, import.meta.env.MODE);
// The build events are tagged with. Build-time on purpose: it has to match the
// release the source maps were uploaded under, and a container variable set
// after the bundle was built could not.
export const SENTRY_RELEASE = import.meta.env.VITE_SENTRY_RELEASE ?? "";
// Cookieless product analytics and error tracking, one key for both. Empty
// means the SDK is never loaded and no PostHog host is contacted. See
// lib/posthog.
export const POSTHOG_KEY = runtimeEnv("POSTHOG_KEY", import.meta.env.VITE_POSTHOG_KEY);
export const POSTHOG_HOST = runtimeEnv("POSTHOG_HOST", import.meta.env.VITE_POSTHOG_HOST, "https://us.i.posthog.com");
// Error tracking is on wherever a key is set. "false" keeps the key for product
// analytics and reports no exceptions, which is what an install that already
// reports to Sentry wants.
// Where the PostHog app itself lives, as opposed to where events are sent.
// They differ whenever api_host is a reverse proxy: without this the SDK builds
// toolbar and session-replay links against the proxy, which does not serve the
// app, so they lead nowhere.
export const POSTHOG_UI_HOST = runtimeEnv("POSTHOG_UI_HOST", import.meta.env.VITE_POSTHOG_UI_HOST, "https://us.posthog.com");
export const POSTHOG_ERROR_TRACKING = runtimeEnv("POSTHOG_ERROR_TRACKING", import.meta.env.VITE_POSTHOG_ERROR_TRACKING, "true") !== "false";
export const HUMAN_VERIFICATION_FAIL = "We couldn’t verify you’re human. Please try the security check again or reload the page.";
export const PASSWORD_FAIL = "The password must be at least 8 characters long and contain both uppercase and lowercase letters, as well as a number."
export const TOKEN_KEY = "auth_token";
export const DEFAULT_PAGINATION_LIMIT = 50;
export const SAVING = "Saving...";
export const REORDERING = "Reordering...";
export const SUCCESS = "Changes successfully changed.";
export const DELETING = "Deleting...";
export const DELETED = "Successfully deleted.";
export const REMOVING = "Removing...";
export const REMOVED = "Successfully removed.";
export const CREATING = "Creating...";
export const CREATED = "Successfully created."
export const ADDING = "Adding..."
export const ADDED = "Successfully added."
