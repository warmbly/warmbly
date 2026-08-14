import { defineConfig, devices } from "@playwright/test";

/**
 * CONFENGE product-acceptance UI path only (scoped exception to AGENTS.md ban).
 * Run: CONFENGE_E2E_BASE_URL=http://127.0.0.1:5173 npx playwright test -c playwright.config.ts
 * Does not run in default CI unless CONFENGE_E2E=1 is set.
 */
export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: 0,
  workers: 1,
  timeout: 90_000,
  reporter: [["list"], ["html", { open: "never", outputFolder: "playwright-report" }]],
  use: {
    baseURL: process.env.CONFENGE_E2E_BASE_URL || "http://127.0.0.1:5173",
    trace: "on-first-retry",
    screenshot: "only-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});
