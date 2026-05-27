// Environment helpers shared across the admin app. Centralized so the
// API base URL and the "PROD / STAGING / DEV" pill always agree.

export const API_URL: string = import.meta.env.VITE_API_URL ?? "http://localhost:8080";

export const DASHBOARD_URL: string =
    import.meta.env.VITE_DASHBOARD_URL ?? "http://localhost:5173";

export type EnvLabel = "production" | "staging" | "development";

const RAW_ENV_LABEL = (import.meta.env.VITE_ENV_LABEL as string | undefined)?.toLowerCase();

export const ENV_LABEL: EnvLabel =
    RAW_ENV_LABEL === "production" || RAW_ENV_LABEL === "prod"
        ? "production"
        : RAW_ENV_LABEL === "staging" || RAW_ENV_LABEL === "stage"
            ? "staging"
            : "development";

export const IS_PRODUCTION = ENV_LABEL === "production";
