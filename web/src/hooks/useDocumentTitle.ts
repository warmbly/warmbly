import { useEffect } from "react";
import { useLocation } from "react-router-dom";

/*
 * Dynamic document titles for the SPA.
 *
 * react-router is used here in declarative/library mode (createBrowserRouter +
 * RouterProvider, no SSR), so the framework-mode `meta` export does not apply.
 * Instead we keep one central route -> label map and set `document.title` on
 * every navigation. Titles read "Section | Warmbly" (mirrors the marketing
 * site's separator); the bare brand is the fallback for unmapped routes.
 *
 * Called once from RootLayout, which renders the <Outlet/> for every route, so
 * a single hook covers auth, onboarding and the whole /app dashboard.
 */

const BRAND = "Warmbly";

// Static routes: exact pathname -> label. Dynamic segments (:id) are handled
// by the parameterised list below.
const ROUTE_TITLES: Record<string, string> = {
  "/": BRAND,

  // Auth
  "/auth/login": "Sign in",
  "/auth/login/confirm": "Verify your email",
  "/auth/register": "Create your account",
  "/auth/register/confirm": "Confirm your email",
  "/auth/reset-password": "Reset your password",
  "/auth/reset-password/confirm": "Set a new password",

  // Onboarding / workspace selection
  "/onboarding": "Welcome",
  "/select-org": "Select workspace",

  // App
  "/app/emails": "Mailboxes",
  "/app/contacts": "Contacts",
  "/app/campaigns": "Campaigns",
  "/app/analytics": "Analytics",
  "/app/crm/pipelines": "Pipelines",
  "/app/crm/deals": "Deals",
  "/app/crm/tasks": "Tasks",
  "/app/templates": "Templates",
  "/app/api-keys": "API keys",
  "/app/integrations": "Integrations",
  "/app/audit": "Audit log",
  "/app/unibox": "Unibox",

  // Settings
  "/app/settings/profile": "Profile",
  "/app/settings/notifications": "Notifications",
  "/app/settings/security": "Security",
  "/app/settings/members": "Members",
  "/app/settings/workspace": "Workspace",
  "/app/settings/billing": "Billing",
  "/app/settings/limits": "Plan & limits",
  "/app/settings/roles": "Roles",
  "/app/settings/danger": "Danger zone",

  // Admin
  "/app/admin": "Admin",
  "/app/admin/workers": "Workers",
  "/app/admin/workers/new": "Add worker",
  "/app/admin/credentials": "Credentials",
  "/app/admin/audit": "Admin audit",
};

// Parameterised routes: [regex, label]. Ordered most-specific first so a
// nested path matches its own entry before the shorter parent pattern.
const PARAM_ROUTES: ReadonlyArray<readonly [RegExp, string]> = [
  [/^\/app\/campaigns\/[^/]+\/leads$/, "Campaign leads"],
  [/^\/app\/campaigns\/[^/]+\/preferences$/, "Campaign settings"],
  [/^\/app\/campaigns\/[^/]+\/schedule$/, "Campaign schedule"],
  [/^\/app\/campaigns\/[^/]+\/sequences$/, "Campaign sequences"],
  [/^\/app\/campaigns\/[^/]+$/, "Campaign"],
  [/^\/app\/admin\/workers\/[^/]+$/, "Worker"],
];

function titleForPath(pathname: string): string {
  const label = ROUTE_TITLES[pathname];
  if (label !== undefined) return label === BRAND ? BRAND : `${label} | ${BRAND}`;

  for (const [pattern, paramLabel] of PARAM_ROUTES) {
    if (pattern.test(pathname)) return `${paramLabel} | ${BRAND}`;
  }
  return BRAND;
}

/**
 * Sets document.title from the current route. Pass an explicit `override` to
 * title a page from loaded data (e.g. a campaign name) instead of the map.
 */
export function useDocumentTitle(override?: string) {
  const { pathname } = useLocation();

  useEffect(() => {
    document.title = override ? `${override} | ${BRAND}` : titleForPath(pathname);
  }, [pathname, override]);
}
