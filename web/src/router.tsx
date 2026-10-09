// The route table: every page is a lazy chunk preloaded on link hover, and every route carries its tab title.

import {
    createRootRouteWithContext,
    createRoute,
    createRouter,
    lazyRouteComponent,
    redirect,
    type AnyRoute,
    type AnyRouter,
    type RouterHistory,
} from "@tanstack/react-router";
import type { QueryClient } from "@tanstack/react-query";
import type { SSOLinkChallenge } from "./lib/api/models/auth/LoginResult";

import RootLayout from "./app/layout";
import NotFound from "./app/not-found";
import { BlankPending, RouteFallback, RouteError, ShellSkeleton } from "./components/layout/RouteStates";
import getToken from "./lib/helper/getToken";
import { bootDashboard, requireDashboardToken } from "./lib/boot";
import { queryClient } from "./lib/queryClient";
import { parseSearch, stringifySearch, type SearchParams } from "./lib/routerSearch";
import { acquisitionSearch } from "./lib/acquisition";
import * as loaders from "./routeLoaders";
import type { PageLoader } from "./routeLoaders";

export interface RouterContext {
    queryClient: QueryClient;
}

declare module "@tanstack/react-router" {
    interface Register {
        router: typeof router;
    }
    // Handed from the SSO landing to the login screen, never put in the URL.
    interface HistoryState {
        two_fa_pending?: string;
        sso_link?: SSOLinkChallenge;
    }
    interface StaticDataRouteOption {
        /** Tab title, without the brand. */
        title?: string;
        /** Params that address state inside the page, not a different page. */
        stableParams?: string[];
    }
}

// Dashboard pages, also warmed in the background once the app is idle so the
// first click on any nav entry never waits on a chunk.
export const dashboardPages = {
    emails: () => import("./app/app/emails/page"),
    domains: () => import("./app/app/emails/domains/page"),
    contactsLayout: () => import("./app/app/contacts/layout"),
    contacts: () => import("./app/app/contacts/page"),
    segments: () => import("./app/app/contacts/segments/page"),
    segment: () => import("./app/app/contacts/segments/[id]/page"),
    labels: () => import("./app/app/contacts/labels/page"),
    suppressions: () => import("./app/app/contacts/suppressions/page"),
    forms: () => import("./app/app/forms/page"),
    form: () => import("./app/app/forms/[id]/page"),
    campaigns: () => import("./app/app/campaigns/page"),
    campaignLayout: () => import("./app/app/campaigns/[id]/layout"),
    campaign: () => import("./app/app/campaigns/[id]/page"),
    campaignLeads: () => import("./app/app/campaigns/[id]/leads/page"),
    campaignPreferences: () => import("./app/app/campaigns/[id]/preferences/page"),
    campaignSchedule: () => import("./app/app/campaigns/[id]/schedule/page"),
    campaignSteps: () => import("./app/app/campaigns/[id]/steps/page"),
    analytics: () => import("./app/app/analytics/page"),
    deliverability: () => import("./app/app/deliverability/page"),
    placement: () => import("./app/app/placement/page"),
    placementTest: () => import("./app/app/placement/[id]/page"),
    placementBatch: () => import("./app/app/placement/batches/[id]/page"),
    pipelines: () => import("./app/app/crm/pipelines/page"),
    deals: () => import("./app/app/crm/deals/page"),
    tasks: () => import("./app/app/crm/tasks/page"),
    meetings: () => import("./app/app/crm/meetings/page"),
    templates: () => import("./app/app/templates/page"),
    apiKeys: () => import("./app/app/api-keys/page"),
    integrations: () => import("./app/app/integrations/page"),
    hubspot: () => import("./app/app/integrations/hubspot/page"),
    pipedrive: () => import("./app/app/integrations/pipedrive/page"),
    salesforce: () => import("./app/app/integrations/salesforce/[id]/page"),
    automations: () => import("./app/app/automations/page"),
    automation: () => import("./app/app/automations/[id]/page"),
    audit: () => import("./app/app/audit/page"),
    unibox: () => import("./app/app/unibox/page"),
    settingsLayout: () => import("./app/app/settings/layout"),
    profile: () => import("./app/app/settings/profile/page"),
    notifications: () => import("./app/app/settings/notifications/page"),
    security: () => import("./app/app/settings/security/page"),
    members: () => import("./app/app/settings/members/page"),
    teams: () => import("./app/app/settings/teams/page"),
    workspace: () => import("./app/app/settings/workspace/page"),
    aiSkills: () => import("./app/app/settings/ai-skills/page"),
    billing: () => import("./app/app/settings/billing/page"),
    referral: () => import("./app/app/settings/referral/page"),
    limits: () => import("./app/app/settings/limits/page"),
    sending: () => import("./app/app/settings/sending/page"),
    tracking: () => import("./app/app/settings/tracking/page"),
    inboxTagging: () => import("./app/app/settings/inbox-tagging/page"),
    roles: () => import("./app/app/settings/roles/page"),
    warmblyCloud: () => import("./app/app/settings/warmbly-cloud/page"),
    oauthApps: () => import("./app/app/settings/oauth-apps/page"),
    webhooks: () => import("./app/app/settings/webhooks/page"),
    connections: () => import("./app/app/settings/connections/page"),
    data: () => import("./app/app/settings/data/page"),
    danger: () => import("./app/app/settings/danger/page"),
    notFound: () => import("./app/app/not-found"),
};

const page = (load: () => Promise<{ default: React.ComponentType }>) => lazyRouteComponent(load);

// The query string is kept as plain strings, so every link already in the
// wild (emails, Slack, bookmarks) keeps resolving.
const rootRoute = createRootRouteWithContext<RouterContext>()({
    component: RootLayout,
    pendingComponent: BlankPending,
    notFoundComponent: NotFound,
    validateSearch: (search: Record<string, unknown>): SearchParams => search as SearchParams,
});

// A route that only forwards somewhere else.
function forward<TParent extends AnyRoute, TPath extends string>(parent: TParent, path: TPath, to: string, keepSearch = false) {
    return createRoute({
        getParentRoute: () => parent,
        path,
        beforeLoad: ({ location }) => {
            throw redirect({ href: keepSearch ? to + location.searchStr : to, replace: true });
        },
    });
}

// Pages for someone who must be signed in, outside the dashboard chrome.
function requireToken({ location }: { location: { href: string; searchStr: string } }) {
    if (getToken()) return;
    throw redirect({ to: "/auth/login", search: { ...acquisitionSearch(location.searchStr), next: location.href }, replace: true });
}

const indexRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/",
    beforeLoad: ({ location }) => {
        throw redirect({ to: "/app/emails", search: acquisitionSearch(location.searchStr), replace: true });
    },
});

// Auth
const authRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "auth",
    pendingComponent: BlankPending,
    component: lazyRouteComponent(() => import("./app/auth/layout")),
});
const loginLayout = lazyRouteComponent(() => import("./app/auth/login/layout"));
const loginPage = lazyRouteComponent(() => import("./app/auth/login/page"));
const registerRoute = createRoute({ getParentRoute: () => authRoute, path: "register", component: loginLayout });
const registerIndex = createRoute({
    getParentRoute: () => registerRoute,
    path: "/",
    component: loginPage,
    staticData: { title: "Create your account" },
});
const registerConfirm = createRoute({
    getParentRoute: () => registerRoute,
    path: "confirm",
    component: lazyRouteComponent(() => import("./app/auth/register/confirm/page")),
    staticData: { title: "Confirm your email" },
});
const loginRoute = createRoute({ getParentRoute: () => authRoute, path: "login", component: loginLayout });
const loginIndex = createRoute({
    getParentRoute: () => loginRoute,
    path: "/",
    component: loginPage,
    staticData: { title: "Sign in" },
});
const loginConfirm = createRoute({
    getParentRoute: () => loginRoute,
    path: "confirm",
    component: lazyRouteComponent(() => import("./app/auth/login/confirm/page")),
    staticData: { title: "Verify your email" },
});
// Landing for the single-use code an SSO redirect carries.
const ssoRoute = createRoute({
    getParentRoute: () => authRoute,
    path: "sso",
    component: lazyRouteComponent(() => import("./app/auth/sso/page")),
    staticData: { title: "Signing you in" },
});
const resetRoute = createRoute({
    getParentRoute: () => authRoute,
    path: "reset-password",
    component: lazyRouteComponent(() => import("./app/auth/reset-password/layout")),
});
const resetIndex = createRoute({
    getParentRoute: () => resetRoute,
    path: "/",
    component: lazyRouteComponent(() => import("./app/auth/reset-password/page")),
    staticData: { title: "Reset your password" },
});
const resetConfirm = createRoute({
    getParentRoute: () => resetRoute,
    path: "confirm",
    component: lazyRouteComponent(() => import("./app/auth/reset-password/confirm/page")),
    staticData: { title: "Set a new password" },
});

const onboardingRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "onboarding",
    pendingComponent: BlankPending,
    beforeLoad: requireToken,
    component: lazyRouteComponent(() => import("./app/onboarding/layout")),
});
const onboardingIndex = createRoute({
    getParentRoute: () => onboardingRoute,
    path: "/",
    component: lazyRouteComponent(() => import("./app/onboarding/page")),
    staticData: { title: "Welcome" },
});

const standalone = <TPath extends string>(path: TPath, load: () => Promise<{ default: React.ComponentType }>, title: string) =>
    createRoute({
        getParentRoute: () => rootRoute,
        path,
        component: lazyRouteComponent(load),
        pendingComponent: BlankPending,
        staticData: { title },
    });

const selectOrgRoute = standalone("select-org", () => import("./app/select-org/page"), "Select workspace");
const inviteRoute = standalone("invite", () => import("./app/invite/page"), "Join workspace");
const connectRoute = standalone("connect", () => import("./app/connect/page"), "Connect");
// Where `warmbly auth login` sends the browser to approve its code.
const cliRoute = standalone("cli", () => import("./app/cli/page"), "Authorize CLI");
// Where Warmbly Cloud sends the Google/Microsoft popup back to on a linked instance.
const cloudOAuthDoneRoute = standalone("cloud-oauth/done", () => import("./app/cloud-oauth/done/page"), "Mailbox connected");
// Where a sign-in window without an opener hands its result to the waiting dashboard tab.
const oauthReturnRoute = standalone("oauth-return", () => import("./app/oauth-return/page"), "Signing in");
// Where the Warmbly app in Slack sends a member to link their account.
const slackLinkRoute = standalone("slack/link", () => import("./app/slack/link/page"), "Link Slack");
// First-run claim link printed by the backend on an empty database.
const setupRoute = standalone("setup", () => import("./app/setup/page"), "Set up Warmbly");

const oauthRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "oauth",
    pendingComponent: BlankPending,
    beforeLoad: requireToken,
    component: lazyRouteComponent(() => import("./app/oauth/layout")),
});
const oauthAuthorize = createRoute({
    getParentRoute: () => oauthRoute,
    path: "authorize",
    component: lazyRouteComponent(() => import("./app/oauth/authorize/page")),
    staticData: { title: "Authorize app" },
});

// The dashboard. Its beforeLoad resolves the session, the workspace and the
// bootstrap data in one parallel round, so pages mount with what they need.
const appRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "app",
    beforeLoad: ({ location }) => requireDashboardToken(location),
    // A loader, not beforeLoad, so the page's chunk downloads alongside it.
    loader: ({ context, location }) => bootDashboard(context.queryClient, location),
    pendingComponent: ShellSkeleton,
    component: lazyRouteComponent(() => import("./app/app/layout")),
    notFoundComponent: lazyRouteComponent(dashboardPages.notFound),
});

const dash = <TParent extends AnyRoute, TPath extends string>(
    parent: TParent,
    path: TPath,
    load: () => Promise<{ default: React.ComponentType }>,
    title?: string,
    loader?: PageLoader,
) => createRoute({ getParentRoute: () => parent, path, component: page(load), staticData: title ? { title } : {}, loader });

// Keeps the query string, so /app?agent_session=… still opens the assistant.
const appIndex = forward(appRoute, "/", "/app/emails", true);

const emailsRoute = createRoute({ getParentRoute: () => appRoute, path: "emails" });
const emailsIndex = dash(emailsRoute, "/", dashboardPages.emails, "Mailboxes", loaders.emailsLoader);
const domainsRoute = dash(emailsRoute, "domains", dashboardPages.domains, "Sending domains", loaders.domainsLoader);

const contactsRoute = createRoute({
    getParentRoute: () => appRoute,
    path: "contacts",
    component: page(dashboardPages.contactsLayout),
});
const contactsIndex = dash(contactsRoute, "/", dashboardPages.contacts, "Contacts", loaders.contactsLoader);
const segmentsRoute = createRoute({ getParentRoute: () => contactsRoute, path: "segments" });
const segmentsIndex = dash(segmentsRoute, "/", dashboardPages.segments, "Segments", loaders.segmentsLoader);
const segmentRoute = dash(segmentsRoute, "$id", dashboardPages.segment, "Segment", loaders.segmentLoader);
const labelsRoute = dash(contactsRoute, "labels", dashboardPages.labels, "Labels", loaders.labelsLoader);
const categoriesRoute = forward(contactsRoute, "categories", "/app/contacts/labels");
const suppressionsRoute = dash(contactsRoute, "suppressions", dashboardPages.suppressions, "Suppression list", loaders.suppressionsLoader);

const formsRoute = createRoute({ getParentRoute: () => appRoute, path: "forms" });
const formsIndex = dash(formsRoute, "/", dashboardPages.forms, "Forms", loaders.formsLoader);
const formRoute = dash(formsRoute, "$id", dashboardPages.form, "Form", loaders.formLoader);

const campaignsRoute = createRoute({ getParentRoute: () => appRoute, path: "campaigns" });
const campaignsIndex = dash(campaignsRoute, "/", dashboardPages.campaigns, "Campaigns", loaders.campaignsLoader);
const campaignRoute = createRoute({
    getParentRoute: () => campaignsRoute,
    path: "$id",
    component: page(dashboardPages.campaignLayout),
    loader: loaders.campaignLoader,
});
const campaignIndex = dash(campaignRoute, "/", dashboardPages.campaign, "Campaign", loaders.campaignOverviewLoader);
const campaignLeads = dash(campaignRoute, "leads", dashboardPages.campaignLeads, "Campaign leads", loaders.campaignLeadsLoader);
const campaignPreferences = dash(campaignRoute, "preferences", dashboardPages.campaignPreferences, "Campaign settings", loaders.campaignPreferencesLoader);
const campaignSchedule = dash(campaignRoute, "schedule", dashboardPages.campaignSchedule, "Campaign schedule", loaders.campaignScheduleLoader);
const campaignSteps = dash(campaignRoute, "steps", dashboardPages.campaignSteps, "Campaign steps", loaders.campaignStepsLoader);

const analyticsRoute = dash(appRoute, "analytics", dashboardPages.analytics, "Analytics", loaders.analyticsLoader);
const deliverabilityRoute = dash(appRoute, "deliverability", dashboardPages.deliverability, "Deliverability", loaders.deliverabilityLoader);

const placementRoute = createRoute({ getParentRoute: () => appRoute, path: "placement" });
const placementIndex = dash(placementRoute, "/", dashboardPages.placement, "Placement tests", loaders.placementLoader);
const placementBatch = dash(placementRoute, "batches/$id", dashboardPages.placementBatch, "Placement batch", loaders.placementBatchLoader);
const placementTest = dash(placementRoute, "$id", dashboardPages.placementTest, "Placement test", loaders.placementTestLoader);

const crmRoute = createRoute({ getParentRoute: () => appRoute, path: "crm" });
const crmIndex = forward(crmRoute, "/", "/app/crm/pipelines");
const pipelinesRoute = dash(crmRoute, "pipelines", dashboardPages.pipelines, "Pipelines", loaders.pipelinesLoader);
const dealsRoute = dash(crmRoute, "deals", dashboardPages.deals, "Deals", loaders.dealsLoader);
const tasksRoute = dash(crmRoute, "tasks", dashboardPages.tasks, "Tasks", loaders.tasksLoader);
const meetingsRoute = dash(crmRoute, "meetings", dashboardPages.meetings, "Meetings", loaders.meetingsLoader);

const templatesRoute = dash(appRoute, "templates", dashboardPages.templates, "Templates", loaders.templatesLoader);
const apiKeysRoute = dash(appRoute, "api-keys", dashboardPages.apiKeys, "API keys", loaders.apiKeysLoader);
// OAuth apps moved into Settings; keep the old path working.
const oauthAppsLegacy = forward(appRoute, "oauth-apps", "/app/settings/oauth-apps");

// The store page is the layout and its children render nothing, so search and drawers survive moving within it.
const integrationsRoute = createRoute({
    getParentRoute: () => appRoute,
    path: "integrations",
    component: page(dashboardPages.integrations),
    staticData: { title: "Integrations", stableParams: ["_splat"] },
});
const integrationsIndex = createRoute({ getParentRoute: () => integrationsRoute, path: "/" });
const integrationsSplat = createRoute({ getParentRoute: () => integrationsRoute, path: "$" });
const hubspotRoute = dash(appRoute, "integrations/hubspot", dashboardPages.hubspot, "HubSpot");
const pipedriveRoute = dash(appRoute, "integrations/pipedrive", dashboardPages.pipedrive, "Pipedrive");
const salesforceRoute = dash(appRoute, "integrations/salesforce/$id", dashboardPages.salesforce, "Salesforce");

const automationsRoute = dash(appRoute, "automations", dashboardPages.automations, "Automations", loaders.automationsLoader);
const automationRoute = dash(appRoute, "automations/$id", dashboardPages.automation, "Automation", loaders.automationLoader);
const auditRoute = dash(appRoute, "audit", dashboardPages.audit, "Audit log", loaders.auditLoader);
// Link buttons sent before the link page moved out of the dashboard.
const appSlackLinkRoute = forward(appRoute, "slack/link", "/slack/link", true);
// The breadcrumb above the link page points here.
const slackRoute = forward(appRoute, "slack", "/app/integrations");

const settingsRoute = createRoute({
    getParentRoute: () => appRoute,
    path: "settings",
    component: page(dashboardPages.settingsLayout),
});
const settingsIndex = forward(settingsRoute, "/", "/app/settings/profile");
const settingsChildren = [
    settingsIndex,
    dash(settingsRoute, "profile", dashboardPages.profile, "Profile"),
    dash(settingsRoute, "notifications", dashboardPages.notifications, "Notifications"),
    dash(settingsRoute, "security", dashboardPages.security, "Security"),
    dash(settingsRoute, "members", dashboardPages.members, "Members", loaders.membersLoader),
    dash(settingsRoute, "teams", dashboardPages.teams, "Teams", loaders.teamsLoader),
    dash(settingsRoute, "workspace", dashboardPages.workspace, "Workspace"),
    dash(settingsRoute, "ai-skills", dashboardPages.aiSkills, "AI skills"),
    dash(settingsRoute, "billing/{-$tab}", dashboardPages.billing, "Billing", loaders.billingLoader),
    dash(settingsRoute, "referral", dashboardPages.referral, "Refer & earn"),
    dash(settingsRoute, "limits", dashboardPages.limits, "Plan & limits"),
    dash(settingsRoute, "sending", dashboardPages.sending, "Sending"),
    dash(settingsRoute, "tracking", dashboardPages.tracking, "Website tracking"),
    dash(settingsRoute, "inbox-tagging", dashboardPages.inboxTagging, "Automatic inbox tagging"),
    dash(settingsRoute, "roles", dashboardPages.roles, "Roles", loaders.rolesLoader),
    dash(settingsRoute, "warmbly-cloud", dashboardPages.warmblyCloud, "Warmbly Cloud"),
    dash(settingsRoute, "oauth-apps", dashboardPages.oauthApps, "OAuth apps"),
    dash(settingsRoute, "webhooks", dashboardPages.webhooks, "Webhooks", loaders.webhooksLoader),
    dash(settingsRoute, "connections", dashboardPages.connections, "Connections"),
    dash(settingsRoute, "data", dashboardPages.data, "Data"),
    dash(settingsRoute, "danger", dashboardPages.danger, "Danger zone"),
] as const;
// Legacy /app/billing entry points.
const billingLegacy = forward(appRoute, "billing", "/app/settings/billing");

// Scope and thread are state inside one page, so the list keeps its scroll offset when a thread opens (issue #396).
const uniboxRoute = createRoute({
    getParentRoute: () => appRoute,
    path: "unibox/{-$scope}/{-$threadId}",
    component: page(dashboardPages.unibox),
    loader: loaders.uniboxLoader,
    staticData: { title: "Unibox", stableParams: ["scope", "threadId"] },
});
// Legacy /app/team entry points.
const teamLegacy = forward(appRoute, "team", "/app/settings/members");

const routeTree = rootRoute.addChildren([
    indexRoute,
    authRoute.addChildren([
        registerRoute.addChildren([registerIndex, registerConfirm]),
        loginRoute.addChildren([loginIndex, loginConfirm]),
        ssoRoute,
        resetRoute.addChildren([resetIndex, resetConfirm]),
    ]),
    onboardingRoute.addChildren([onboardingIndex]),
    selectOrgRoute,
    inviteRoute,
    connectRoute,
    slackLinkRoute,
    cliRoute,
    cloudOAuthDoneRoute,
    oauthReturnRoute,
    setupRoute,
    oauthRoute.addChildren([oauthAuthorize]),
    appRoute.addChildren([
        appIndex,
        emailsRoute.addChildren([emailsIndex, domainsRoute]),
        contactsRoute.addChildren([
            contactsIndex,
            segmentsRoute.addChildren([segmentsIndex, segmentRoute]),
            labelsRoute,
            categoriesRoute,
            suppressionsRoute,
        ]),
        formsRoute.addChildren([formsIndex, formRoute]),
        campaignsRoute.addChildren([
            campaignsIndex,
            campaignRoute.addChildren([campaignIndex, campaignLeads, campaignPreferences, campaignSchedule, campaignSteps]),
        ]),
        analyticsRoute,
        deliverabilityRoute,
        placementRoute.addChildren([placementIndex, placementBatch, placementTest]),
        crmRoute.addChildren([crmIndex, pipelinesRoute, dealsRoute, tasksRoute, meetingsRoute]),
        templatesRoute,
        apiKeysRoute,
        oauthAppsLegacy,
        integrationsRoute.addChildren([integrationsIndex, integrationsSplat]),
        hubspotRoute,
        pipedriveRoute,
        salesforceRoute,
        automationsRoute,
        automationRoute,
        auditRoute,
        appSlackLinkRoute,
        slackRoute,
        settingsRoute.addChildren(settingsChildren),
        billingLegacy,
        uniboxRoute,
        teamLegacy,
    ]),
]);

/** The app's router; tests pass a memory history to mount the real route tree. */
export function createAppRouter(queryClient: QueryClient, history?: RouterHistory) {
    const created = createRouter({
        routeTree,
        history,
        context: { queryClient },
        parseSearch,
        stringifySearch,
        // Hovering or focusing a link loads its page and data before the click.
        defaultPreload: "intent",
        defaultPreloadDelay: 40,
        // react-query owns freshness; the router only has to start the fetch.
        defaultPreloadStaleTime: 0,
        // Loaders never wait on data, so only a page's code can hold a click;
        // that is warmed in the background, and a skeleton shows only past 100ms.
        defaultPendingMs: 100,
        defaultPendingMinMs: 150,
        defaultPendingComponent: RouteFallback,
        defaultErrorComponent: RouteError,
        // A route with new params is a new page, except for the params a route
        // marks as in-page state.
        defaultRemountDeps: ({ routeId, params }) => remountKey(created, routeId, params),
        defaultStructuralSharing: true,
    });
    return created;
}

function remountKey(r: AnyRouter, routeId: string, params: Record<string, unknown>) {
    const stable: string[] = r.routesById[routeId]?.options.staticData?.stableParams ?? [];
    return Object.keys(params)
        .filter((k) => !stable.includes(k))
        .sort()
        .map((k) => `${k}=${String(params[k] ?? "")}`)
        .join("&");
}

export const router = createAppRouter(queryClient);
