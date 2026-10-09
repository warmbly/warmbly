// Route loaders: start each page's first queries, under its hooks' exact keys, on hover and on click.

import type { QueryClient } from "@tanstack/react-query";
import type { RouterContext } from "./router";

import { dashboardReady } from "./lib/boot";
import { useAppStore } from "./stores/useAppStore";
import { isAccessRestricted, orgHasPermission, type PermissionKey } from "./hooks/usePermission";
import { isScopedPath } from "./lib/accessScope";
import { hasPermission, PERMISSION_BITS } from "./lib/permissions";
import { loadCampaignPeriod, periodWindow, utcToday } from "./lib/campaignPeriod";
import { parseSearch, type SearchParams } from "./lib/routerSearch";
import { scopeSearch } from "./components/app/contacts/filters/helpers";
import type { ColumnViewName } from "./lib/api/models/app/views/ViewPreferences";
import type { SearchContactsSortBy } from "./lib/api/models/app/contacts/search-contacts.types";
import type { UniboxFolder, UniboxSearchParams } from "./lib/api/models/app/unibox/UniboxSearch";
import { EMPTY_DEAL_SEARCH } from "./lib/api/models/app/crm/SearchDeals";
import { EMPTY_TASK_SEARCH } from "./lib/api/models/app/crm/SearchTasks";

import { userQuery } from "./lib/api/hooks/auth/useUser";
import { authConfigQuery } from "./lib/api/hooks/auth/useAuthConfig";
import { emailsListQuery } from "./lib/api/hooks/app/emails/useEmails";
import { sendingDomainsQuery } from "./lib/api/hooks/app/emails/useSendingDomains";
import { signinMigrationQuery } from "./lib/api/hooks/app/emails/useMailboxGrants";
import { featureStatusQuery } from "./lib/api/hooks/app/subscription/useFeatureStatus";
import { advisorFindingsQuery, advisorSurfaceQuery } from "./lib/api/hooks/app/advisor/useAdvisor";
import { contactsSearchQuery } from "./lib/api/hooks/app/contacts/useSearchContacts";
import { customFieldKeysQuery } from "./lib/api/hooks/app/contacts/useCustomFieldKeys";
import { readCachedView, viewPreferencesQuery } from "./lib/api/hooks/app/views/useViewPreferences";
import { labelCountQuery, segmentFieldsQuery, segmentQuery, segmentsListQuery } from "./lib/api/hooks/app/segments";
import { suppressionsListQuery } from "./lib/api/hooks/app/suppressions/useSuppressions";
import { formQuery, formsListQuery } from "./lib/api/hooks/app/forms";
import { campaignsListQuery } from "./lib/api/hooks/app/campaigns/useCampaigns";
import { campaignQuery } from "./lib/api/hooks/app/campaigns/useCampaign";
import { campaignSendersQuery } from "./lib/api/hooks/app/campaigns/useCampaignSenders";
import { sequencesQuery } from "./lib/api/hooks/app/campaigns/sequences/useSequences";
import { campaignAnalyticsQuery } from "./lib/api/hooks/app/analytics/useCampaignAnalytics";
import { campaignDailyStatsQuery } from "./lib/api/hooks/app/analytics/useCampaignDailyStats";
import { dashboardQuery } from "./lib/api/hooks/app/analytics/useDashboard";
import { deliverabilityQuery } from "./lib/api/hooks/app/analytics/useDeliverability";
import { usageOverviewQuery } from "./lib/api/hooks/app/analytics/useUsageOverview";
import {
    placementBatchQuery,
    placementBatchSendersQuery,
    placementBatchesQuery,
    placementCoverageQuery,
    placementOverviewQuery,
    placementTestQuery,
    placementTestsQuery,
} from "./lib/api/hooks/app/placement/usePlacement";
import { pipelinesQuery } from "./lib/api/hooks/app/crm/pipelines/usePipelines";
import { dealsSearchQuery } from "./lib/api/hooks/app/crm/deals/useSearchDeals";
import { dealsSummaryQuery } from "./lib/api/hooks/app/crm/deals/useDealsSummary";
import { tasksSearchQuery } from "./lib/api/hooks/app/crm/tasks/useSearchTasks";
import { tasksSummaryQuery } from "./lib/api/hooks/app/crm/tasks/useTasksSummary";
import { taskTypesQuery } from "./lib/api/hooks/app/crm/taskTypes/useTaskTypes";
import { meetingsSearchQuery } from "./lib/api/hooks/app/meetings/useSearchMeetings";
import { meetingsSummaryQuery } from "./lib/api/hooks/app/meetings/useMeetingsSummary";
import { membersQuery } from "./lib/api/hooks/app/organizations/useMembers";
import { pendingInvitationsQuery } from "./lib/api/hooks/app/organizations/usePendingInvitations";
import { rolesQuery } from "./lib/api/hooks/app/organizations/useRoles";
import { currentOrganizationQuery } from "./lib/api/hooks/app/organizations/useCurrentOrganization";
import { organizationLimitsQuery } from "./lib/api/hooks/app/organizations/useOrganizationLimits";
import { teamsQuery } from "./lib/api/hooks/app/teams/useTeams";
import { templatesListQuery } from "./lib/api/hooks/app/templates/useTemplates";
import { apiKeysListQuery } from "./lib/api/hooks/app/api-keys/useAPIKeys";
import { apiKeyUsageSummaryQuery } from "./lib/api/hooks/app/api-keys/useAPIKeyUsageSummary";
import { apiKeyAnalyticsQuery } from "./lib/api/hooks/app/api-keys/useAPIKeyAnalytics";
import { automationsQuery } from "./lib/api/hooks/app/automations/useAutomations";
import { automationQuery } from "./lib/api/hooks/app/automations/useAutomation";
import { integrationConnectionsQuery } from "./lib/api/hooks/app/integrations/useIntegrationConnections";
import { integrationCatalogQuery } from "./lib/api/hooks/app/integrations/useIntegrationCatalog";
import { auditLogsQuery } from "./lib/api/hooks/app/audit/useAuditLogs";
import { uniboxOverviewQuery } from "./lib/api/hooks/app/unibox/useUniboxOverview";
import { uniboxSearchQuery } from "./lib/api/hooks/app/unibox/useUniboxSearch";
import { webhookDropsQuery, webhooksListQuery } from "./lib/api/hooks/app/webhooks/useWebhooks";
import { trialStatusQuery } from "./lib/api/hooks/app/subscription/useTrialStatus";
import { appliedDiscountsQuery } from "./lib/api/hooks/app/subscription/useAppliedDiscounts";

// Declaring `params` here would become every route's inferred params; the router passes it anyway.
export interface PageLoaderContext {
    context: unknown;
    location: { pathname: string; searchStr: string };
    cause: "preload" | "enter" | "stay";
}

export type PageLoader = (ctx: PageLoaderContext) => Promise<void>;

// Prefetches never reject, so nothing awaits them.
type Prefetch = Promise<unknown> | false | null | undefined;

// Navigation never waits on data; only the session's workspace is awaited, so org-scoped reads go to the right one.
interface PageArgs {
    params: Record<string, string | undefined>;
    search: SearchParams;
}

function pageLoader(prefetch: (qc: QueryClient, page: PageArgs) => Prefetch[]): PageLoader {
    return async (ctx) => {
        try {
            await dashboardReady();
        } catch {
            return;
        }
        // A restricted member is sent elsewhere; the page's reads would be refused.
        const org = useAppStore.getState().currentOrganization;
        if (isAccessRestricted(org) && !isScopedPath(ctx.location.pathname)) return;
        prefetch((ctx.context as RouterContext).queryClient, {
            params: (ctx as PageLoaderContext & { params?: Record<string, string | undefined> }).params ?? {},
            search: parseSearch(ctx.location.searchStr),
        });
    };
}

const can = (key: PermissionKey) => orgHasPermission(useAppStore.getState().currentOrganization, key);
const restricted = () => isAccessRestricted(useAppStore.getState().currentOrganization);

// Mirrors useFeatureAccess().canManage.
function canManageTeam(): boolean {
    const org = useAppStore.getState().currentOrganization;
    return org?.role === "owner" || org?.role === "admin" || hasPermission(org?.permissions, PERMISSION_BITS.MANAGE_TEAM);
}

// ContactsTable's first request: its scope plus the browser's copy of the
// saved sort, read with the same store identity the table sees.
function contactsList(qc: QueryClient, view: ColumnViewName, scope: { campaignId?: string; segmentId?: string; category?: string }): Prefetch[] {
    const state = useAppStore.getState();
    const orgId = state.currentOrganization?.id ?? "";
    const cached = readCachedView({ userId: state.user?.id ?? "", orgId }, view)?.sort;
    const options = {
        ...scopeSearch({ campaignId: scope.campaignId, segmentId: scope.segmentId }),
        category_ids: scope.category && !scope.segmentId && !scope.campaignId ? [scope.category] : undefined,
        ...(cached ? { sort_by: cached.by as SearchContactsSortBy, reverse: cached.reverse } : {}),
    };
    const userId = state.user?.id ?? qc.getQueryData(userQuery.queryKey)?.id ?? "";
    return [
        qc.prefetchInfiniteQuery(contactsSearchQuery({ options })),
        !!userId && !!orgId && qc.prefetchQuery(viewPreferencesQuery(view, { userId, orgId })),
        qc.prefetchQuery(customFieldKeysQuery),
    ];
}

export const emailsLoader = pageLoader((qc) => [
    can("MANAGE_EMAILS") && qc.prefetchInfiniteQuery(emailsListQuery({ query: "", tag: "" })),
    can("MANAGE_EMAILS") && qc.prefetchQuery(signinMigrationQuery),
    qc.prefetchQuery(advisorSurfaceQuery("emails")),
    qc.prefetchQuery(featureStatusQuery),
]);

export const domainsLoader = pageLoader((qc) => [can("MANAGE_EMAILS") && qc.prefetchQuery(sendingDomainsQuery)]);

export const contactsLoader = pageLoader((qc, { search }) =>
    can("VIEW_CONTACTS")
        ? [...contactsList(qc, "contacts", { category: search.category }), qc.prefetchQuery(advisorSurfaceQuery("contacts"))]
        : [],
);

export const segmentsLoader = pageLoader((qc) => [can("VIEW_CONTACTS") && qc.prefetchQuery(segmentsListQuery)]);

export const segmentLoader = pageLoader((qc, { params }) =>
    can("VIEW_CONTACTS")
        ? [
              qc.prefetchQuery(segmentQuery(params.id)),
              qc.prefetchQuery(segmentFieldsQuery),
              ...contactsList(qc, "contacts", { segmentId: params.id }),
          ]
        : [],
);

export const labelsLoader = pageLoader((qc) => {
    if (!can("VIEW_CONTACTS")) return [];
    const categories = qc.getQueryData(userQuery.queryKey)?.categories ?? [];
    return categories.map((c) => qc.prefetchQuery(labelCountQuery(c.id)));
});

export const suppressionsLoader = pageLoader((qc) => [
    can("VIEW_CONTACTS") && qc.prefetchInfiniteQuery(suppressionsListQuery("")),
]);

export const formsLoader = pageLoader((qc) => [can("VIEW_CONTACTS") && qc.prefetchQuery(formsListQuery)]);

export const formLoader = pageLoader((qc, { params }) => [can("VIEW_CONTACTS") && qc.prefetchQuery(formQuery(params.id))]);

export const campaignsLoader = pageLoader((qc) =>
    can("VIEW_CAMPAIGNS")
        ? [
              qc.prefetchInfiniteQuery(campaignsListQuery({ query: "", folder: "" })),
              !restricted() && qc.prefetchQuery(advisorSurfaceQuery("campaigns")),
          ]
        : [],
);

// The layout's header; each tab's loader adds what the tab draws.
export const campaignLoader = pageLoader((qc, { params }) => [!!params.id && qc.prefetchQuery(campaignQuery(params.id))]);

export const campaignOverviewLoader = pageLoader((qc, { params }) => {
    const id = params.id;
    if (!id) return [];
    const today = utcToday();
    const asked = periodWindow(loadCampaignPeriod(), today);
    // All time charts from the campaign's creation day, which only the campaign knows.
    const daily = asked
        ? qc.prefetchQuery(campaignDailyStatsQuery(id, asked))
        : qc.prefetchQuery(campaignQuery(id)).then(() => {
              const c = qc.getQueryData(campaignQuery(id).queryKey);
              const created = c?.created_at ? new Date(c.created_at) : null;
              const createdDay = created && !Number.isNaN(created.getTime()) ? created.toISOString().slice(0, 10) : null;
              return createdDay ? qc.prefetchQuery(campaignDailyStatsQuery(id, { from: createdDay, to: today })) : undefined;
          });
    return [
        qc.prefetchQuery(campaignAnalyticsQuery(id, asked)),
        daily,
        !restricted() && qc.prefetchQuery(advisorFindingsQuery({ entityType: "campaign", entityId: id, limit: 7 })),
    ];
});

export const campaignLeadsLoader = pageLoader((qc, { params }) =>
    params.id ? contactsList(qc, "campaign_leads", { campaignId: params.id }) : [],
);

export const campaignPreferencesLoader = pageLoader((qc, { params }) => [
    !!params.id && qc.prefetchQuery(campaignSendersQuery(params.id)),
]);

export const campaignScheduleLoader = pageLoader((qc) => [qc.prefetchQuery(currentOrganizationQuery)]);

export const campaignStepsLoader = pageLoader((qc, { params }) => [!!params.id && qc.prefetchQuery(sequencesQuery(params.id))]);

export const analyticsLoader = pageLoader((qc) => [can("VIEW_ANALYTICS") && qc.prefetchQuery(dashboardQuery("7d"))]);

// Mirrors the deliverability page's persisted window.
function deliverabilityRange(): "7d" | "30d" | "90d" {
    try {
        const raw = localStorage.getItem("warmbly:deliverability-view");
        const range = raw ? (JSON.parse(raw) as { range?: unknown } | null)?.range : undefined;
        return range === "30d" || range === "90d" ? range : "7d";
    } catch {
        return "7d";
    }
}

export const deliverabilityLoader = pageLoader((qc) => [qc.prefetchQuery(deliverabilityQuery(deliverabilityRange()))]);

export const placementLoader = pageLoader((qc, { search }) => {
    const tab = search.tab;
    const campaignId = search.campaign_id ?? null;
    const onTests = tab !== "seeds" && tab !== "batches";
    return [
        qc.prefetchQuery(placementOverviewQuery),
        qc.prefetchInfiniteQuery(placementBatchesQuery()),
        tab !== "seeds" && qc.prefetchQuery(placementCoverageQuery),
        onTests && qc.prefetchInfiniteQuery(placementTestsQuery(campaignId)),
        onTests && !!campaignId && qc.prefetchQuery(campaignQuery(campaignId)),
    ];
});

export const placementTestLoader = pageLoader((qc, { params }) => [!!params.id && qc.prefetchQuery(placementTestQuery(params.id))]);

export const placementBatchLoader = pageLoader((qc, { params }) =>
    params.id
        ? [
              qc.prefetchQuery(placementBatchQuery(params.id)),
              qc.prefetchInfiniteQuery(placementBatchSendersQuery(params.id, { sort: "worst", status: "", q: "" })),
          ]
        : [],
);

export const pipelinesLoader = pageLoader((qc) => [qc.prefetchQuery(pipelinesQuery)]);

export const dealsLoader = pageLoader((qc) => [
    qc.prefetchQuery(pipelinesQuery),
    qc.prefetchQuery(membersQuery),
    qc.prefetchInfiniteQuery(dealsSearchQuery({ filters: EMPTY_DEAL_SEARCH, limit: 50 })),
    qc.prefetchQuery(dealsSummaryQuery(EMPTY_DEAL_SEARCH)),
]);

export const tasksLoader = pageLoader((qc) => [
    qc.prefetchInfiniteQuery(tasksSearchQuery({ filters: EMPTY_TASK_SEARCH, limit: 50 })),
    qc.prefetchQuery(tasksSummaryQuery(EMPTY_TASK_SEARCH)),
    qc.prefetchQuery(membersQuery),
    qc.prefetchQuery(teamsQuery),
    qc.prefetchQuery(taskTypesQuery),
]);

export const meetingsLoader = pageLoader((qc) => [
    qc.prefetchInfiniteQuery(meetingsSearchQuery({ filters: { timeframe: "upcoming", q: undefined } })),
    qc.prefetchQuery(meetingsSummaryQuery),
]);

export const templatesLoader = pageLoader((qc) => [qc.prefetchQuery(templatesListQuery(undefined))]);

export const apiKeysLoader = pageLoader((qc) =>
    can("MANAGE_API_KEYS")
        ? [
              qc.prefetchQuery(apiKeyUsageSummaryQuery),
              qc.prefetchQuery(apiKeysListQuery),
              qc.prefetchQuery(apiKeyAnalyticsQuery("all")),
          ]
        : [],
);

export const automationsLoader = pageLoader((qc) => [
    qc.prefetchQuery(automationsQuery),
    qc.prefetchQuery(integrationConnectionsQuery),
]);

// The builder waits for all three before it mounts its canvas.
export const automationLoader = pageLoader((qc, { params }) => [
    !!params.id && qc.prefetchQuery(automationQuery(params.id)),
    qc.prefetchQuery(integrationConnectionsQuery),
    qc.prefetchQuery(integrationCatalogQuery),
]);

export const auditLoader = pageLoader((qc) => [canManageTeam() && qc.prefetchQuery(auditLogsQuery({ limit: 50 }))]);

const UNIBOX_FOLDER_SCOPES: UniboxFolder[] = ["inbox", "sent", "drafts", "archive", "spam", "trash"];

// The unibox page's first search for a scope, or null for the scopes whose
// request depends on the clock, the mailbox directory or loaded labels.
function uniboxFirstSearch(urlScope: string, ref: string | undefined): [UniboxSearchParams, string] | null {
    const base: UniboxSearchParams = { sortBy: "newest" };
    if ((UNIBOX_FOLDER_SCOPES as string[]).includes(urlScope)) {
        const folder = urlScope as UniboxFolder;
        return [{ ...base, folder, ...(folder === "inbox" ? { automated: false } : {}) }, `folder:${folder}`];
    }
    switch (urlScope) {
        case "unread":
            return [{ ...base, unseen: true, automated: false }, "unread"];
        case "awaiting":
            return [{ ...base, awaitingReply: true }, "awaiting"];
        case "agent_drafts":
            return [{ ...base, agentDrafts: true }, "agent_drafts"];
        case "snoozed":
            return [{ ...base, snoozed: true }, "snoozed"];
        case "mailbox":
            return ref ? [{ ...base, accountIds: [ref], automated: false }, `mailbox:${ref}`] : allMail(base);
        case "category":
            return ref ? [{ ...base, categoryIds: [ref] }, `category:${ref}`] : allMail(base);
        case "today":
        case "week":
        case "scheduled":
        case "tag":
        case "view":
            return null;
        default:
            return allMail(base);
    }
}

function allMail(base: UniboxSearchParams): [UniboxSearchParams, string] {
    return [{ ...base, includeArchived: true }, "all"];
}

// Cold cache only: opening a thread is a new match, and a stale refetch here would reload every loaded page (#396).
export const uniboxLoader = pageLoader((qc, { params, search }) => {
    if (!can("ACCESS_UNIBOX")) return [];
    const first = uniboxFirstSearch(params.scope ?? "inbox", search.ref || undefined);
    const list = first ? uniboxSearchQuery(...first) : null;
    return [
        qc.getQueryData(uniboxOverviewQuery.queryKey) === undefined && qc.prefetchQuery(uniboxOverviewQuery),
        !!list && qc.getQueryData(list.queryKey) === undefined && qc.prefetchInfiniteQuery(list),
    ];
});

export const membersLoader = pageLoader((qc) => [
    qc.prefetchQuery(membersQuery),
    qc.prefetchQuery(pendingInvitationsQuery),
    qc.prefetchQuery(rolesQuery),
]);

export const teamsLoader = pageLoader((qc) => [qc.prefetchQuery(teamsQuery), qc.prefetchQuery(membersQuery)]);

export const rolesLoader = pageLoader((qc) => [canManageTeam() && qc.prefetchQuery(rolesQuery)]);

export const webhooksLoader = pageLoader((qc) =>
    can("MANAGE_SETTINGS") ? [qc.prefetchQuery(webhooksListQuery), qc.prefetchQuery(webhookDropsQuery)] : [],
);

// The overview tab, for the owner, on a deployment that bills.
export const billingLoader = pageLoader((qc, { params }) => {
    if (params.tab) return [];
    if (useAppStore.getState().currentOrganization?.role !== "owner") return [];
    if (qc.getQueryData(authConfigQuery.queryKey)?.billing_enabled === false) return [];
    return [
        qc.prefetchQuery(trialStatusQuery),
        qc.prefetchQuery(organizationLimitsQuery),
        qc.prefetchQuery(usageOverviewQuery("month")),
        qc.prefetchQuery(appliedDiscountsQuery()),
        can("MANAGE_API_KEYS") && qc.prefetchQuery(apiKeyUsageSummaryQuery),
    ];
});
