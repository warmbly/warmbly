// Environment helpers shared across the admin app. Centralized so the
// API base URL and the "PROD / STAGING / DEV" pill always agree.
//
// These read the container-injected runtime config first (so one built image
// works for any deployment), then the value Vite baked at build time.
import { runtimeEnv } from "./runtimeConfig";

export const API_URL: string = runtimeEnv("API_URL", import.meta.env.VITE_API_URL, "http://localhost:8080");

export const DASHBOARD_URL: string = runtimeEnv("DASHBOARD_URL", import.meta.env.VITE_DASHBOARD_URL, "http://localhost:5173");

export const TURNSTILE_KEY: string = runtimeEnv("TURNSTILE_KEY", import.meta.env.VITE_TURNSTILE_KEY);

// Browser analytics, session replay and error reporting. The admin panel ships
// to self-hosters like every other image, so the credentials are the
// operator's and configuring neither backend means no SDK is ever loaded. See
// lib/observability.
export const POSTHOG_KEY: string = runtimeEnv("POSTHOG_KEY", import.meta.env.VITE_POSTHOG_KEY);
export const POSTHOG_HOST: string = runtimeEnv("POSTHOG_HOST", import.meta.env.VITE_POSTHOG_HOST, "https://us.i.posthog.com");
// Where the PostHog app itself lives, as opposed to where events are sent.
// They differ whenever api_host is a reverse proxy: without this the SDK builds
// toolbar and session-replay links against the proxy, which does not serve the
// app, so they lead nowhere.
export const POSTHOG_UI_HOST: string = runtimeEnv("POSTHOG_UI_HOST", import.meta.env.VITE_POSTHOG_UI_HOST, "https://us.posthog.com");
// Error tracking is on wherever a key is set. "false" turns it off for an
// install that reports to Sentry instead.
export const POSTHOG_ERROR_TRACKING: boolean = runtimeEnv("POSTHOG_ERROR_TRACKING", import.meta.env.VITE_POSTHOG_ERROR_TRACKING, "true") !== "false";
// Session replay is on wherever a key is set. "false" keeps everything else
// and records no sessions.
export const POSTHOG_SESSION_REPLAY: boolean = runtimeEnv("POSTHOG_SESSION_REPLAY", import.meta.env.VITE_POSTHOG_SESSION_REPLAY, "true") !== "false";
export const SENTRY_DSN: string = runtimeEnv("SENTRY_DSN", import.meta.env.VITE_SENTRY_DSN);
// The deployment label and the build every reported event is tagged with,
// whichever backend receives it. The names stay SENTRY_* because they are the
// container variables deployments already set.
export const SENTRY_ENVIRONMENT: string = runtimeEnv("SENTRY_ENVIRONMENT", import.meta.env.VITE_SENTRY_ENVIRONMENT, import.meta.env.MODE);
// Build-time on purpose: it has to match the release the source maps were
// uploaded under, which a container variable set afterwards could not.
export const SENTRY_RELEASE: string = import.meta.env.VITE_SENTRY_RELEASE ?? "";

export type EnvLabel = "production" | "staging" | "development";

const RAW_ENV_LABEL = runtimeEnv("ENV_LABEL", import.meta.env.VITE_ENV_LABEL as string | undefined).toLowerCase();

export const ENV_LABEL: EnvLabel =
    RAW_ENV_LABEL === "production" || RAW_ENV_LABEL === "prod"
        ? "production"
        : RAW_ENV_LABEL === "staging" || RAW_ENV_LABEL === "stage"
            ? "staging"
            : "development";

export const IS_PRODUCTION = ENV_LABEL === "production";
