// Environment helpers shared across the admin app. Centralized so the
// API base URL and the "PROD / STAGING / DEV" pill always agree.
//
// These read the container-injected runtime config first (so one built image
// works for any deployment), then the value Vite baked at build time.
import { runtimeEnv } from "./runtimeConfig";

export const API_URL: string = runtimeEnv("API_URL", import.meta.env.VITE_API_URL, "http://localhost:8080");

export const DASHBOARD_URL: string = runtimeEnv("DASHBOARD_URL", import.meta.env.VITE_DASHBOARD_URL, "http://localhost:5173");

export const TURNSTILE_KEY: string = runtimeEnv("TURNSTILE_KEY", import.meta.env.VITE_TURNSTILE_KEY);

export type EnvLabel = "production" | "staging" | "development";

const RAW_ENV_LABEL = runtimeEnv("ENV_LABEL", import.meta.env.VITE_ENV_LABEL as string | undefined).toLowerCase();

export const ENV_LABEL: EnvLabel =
    RAW_ENV_LABEL === "production" || RAW_ENV_LABEL === "prod"
        ? "production"
        : RAW_ENV_LABEL === "staging" || RAW_ENV_LABEL === "stage"
            ? "staging"
            : "development";

export const IS_PRODUCTION = ENV_LABEL === "production";
