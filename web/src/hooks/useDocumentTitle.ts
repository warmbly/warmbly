import { useEffect } from "react";
import { useLocation } from "react-router-dom";
import { useCurrentOrg, useUnseenCount } from "@/stores";
import { setFaviconBadge } from "@/lib/faviconBadge";

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
  "/auth/sso": "Signing you in",

  // Onboarding / workspace selection
  "/onboarding": "Welcome",
  "/select-org": "Select workspace",
  "/invite": "Join workspace",
  "/setup": "Set up Warmbly",
  "/oauth/authorize": "Authorize app",
  "/cloud-oauth/done": "Mailbox connected",

  // App
  "/app/emails": "Mailboxes",
  "/app/contacts": "Contacts",
  "/app/contacts/segments": "Segments",
  "/app/contacts/categories": "Categories",
  "/app/contacts/suppressions": "Suppression list",
  "/app/campaigns": "Campaigns",
  "/app/analytics": "Analytics",
  "/app/deliverability": "Deliverability",
  "/app/crm/pipelines": "Pipelines",
  "/app/crm/deals": "Deals",
  "/app/crm/tasks": "Tasks",
  "/app/crm/meetings": "Meetings",
  "/app/templates": "Templates",
  "/app/automations": "Automations",
  "/app/forms": "Forms",
  "/app/api-keys": "API keys",
  "/app/oauth-apps": "OAuth apps",
  "/app/integrations": "Integrations",
  "/app/audit": "Audit log",
  "/app/unibox": "Unibox",

  // Settings
  "/app/settings/profile": "Profile",
  "/app/settings/warmbly-cloud": "Warmbly Cloud",
  "/connect": "Connect",
  "/cli": "Authorize CLI",
  "/app/settings/notifications": "Notifications",
  "/app/settings/security": "Security",
  "/app/settings/members": "Members",
  "/app/settings/teams": "Teams",
  "/app/settings/workspace": "Workspace",
  "/app/settings/sending": "Sending",
  "/app/settings/tracking": "Website tracking",
  "/app/settings/ai-skills": "AI skills",
  "/app/settings/billing": "Billing",
  "/app/settings/referral": "Refer & earn",
  "/app/settings/limits": "Plan & limits",
  "/app/settings/roles": "Roles",
  "/app/settings/oauth-apps": "OAuth apps",
  "/app/settings/webhooks": "Webhooks",
  "/app/settings/connections": "Connections",
  "/app/settings/data": "Data",
  "/app/settings/danger": "Danger zone",
};

// Parameterised routes: [regex, label]. Ordered most-specific first so a
// nested path matches its own entry before the shorter parent pattern.
const PARAM_ROUTES: ReadonlyArray<readonly [RegExp, string]> = [
  [/^\/app\/contacts\/segments\/[^/]+$/, "Segment"],
  [/^\/app\/campaigns\/[^/]+\/leads$/, "Campaign leads"],
  [/^\/app\/campaigns\/[^/]+\/preferences$/, "Campaign settings"],
  [/^\/app\/campaigns\/[^/]+\/schedule$/, "Campaign schedule"],
  [/^\/app\/campaigns\/[^/]+\/steps$/, "Campaign steps"],
  [/^\/app\/campaigns\/[^/]+$/, "Campaign"],
  [/^\/app\/automations\/[^/]+$/, "Automation"],
  [/^\/app\/forms\/[^/]+$/, "Form"],
  [/^\/app\/contacts\/segments\/[^/]+$/, "Segment"],
  [/^\/app\/unibox(\/.*)?$/, "Unibox"],
  [/^\/app\/settings\/billing\/[^/]+$/, "Billing"],
  [/^\/app\/admin\/workers\/[^/]+$/, "Worker"],
];

function titleForPath(pathname: string): string {
  const label = ROUTE_TITLES[pathname];
  if (label !== undefined) return label === BRAND ? BRAND : `${label} | ${BRAND}`;

  for (const [pattern, paramLabel] of PARAM_ROUTES) {
    if (pattern.test(pathname)) return `${paramLabel} | ${BRAND}`;
  }
  // Unmatched pathname = a genuine 404; surface that in the tab title.
  return `Page not found | ${BRAND}`;
}

// Fold the current workspace in as context, before the brand:
//   "Mailboxes | Warmbly"  ->  "Mailboxes · Acme | Warmbly"
//   "Warmbly"              ->  "Acme | Warmbly"
function withOrg(base: string, org?: string): string {
  if (!org) return base;
  const suffix = ` | ${BRAND}`;
  if (base === BRAND) return `${org}${suffix}`;
  if (base.endsWith(suffix)) return `${base.slice(0, -suffix.length)} · ${org}${suffix}`;
  return `${base} · ${org}`;
}

/**
 * Sets document.title from the current route, folding in the current workspace
 * and a leading unread-count prefix ("(3) …") on the dashboard, and mirrors the
 * unread count onto the favicon as a red badge. Pass an explicit `override` to
 * title a page from loaded data (e.g. a campaign name) instead of the map.
 */
export function useDocumentTitle(override?: string) {
  const { pathname } = useLocation();
  const org = useCurrentOrg();
  const unread = useUnseenCount();

  useEffect(() => {
    // Workspace context + the unread badge are dashboard-only; on auth /
    // marketing routes (or with no selected workspace) use the plain title.
    const onApp = pathname.startsWith("/app");
    const count = onApp ? unread : 0;

    const base = override ? `${override} | ${BRAND}` : titleForPath(pathname);
    const titled = withOrg(base, onApp ? org?.name : undefined);
    const prefix = count > 0 ? `(${count > 99 ? "99+" : count}) ` : "";
    document.title = `${prefix}${titled}`;

    setFaviconBadge(count);
  }, [pathname, override, org?.name, unread]);
}
