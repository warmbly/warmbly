// The agency home and the cross-client inbox: queries, live refresh and the
// "open this client" switch they share.

import { useEffect, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import {
    infiniteQueryOptions,
    queryOptions,
    useInfiniteQuery,
    useQuery,
    useQueryClient,
    type InfiniteData,
    type Query,
} from "@tanstack/react-query";
import toast from "react-hot-toast/headless";
import {
    getAgencyInboxMessage,
    getAgencyInboxThread,
    getAgencyOverview,
    listAgencyInbox,
} from "@/lib/api/client/app/agency/agencyHome";
import type { AgencyInboxFilters, AgencyInboxPage } from "@/lib/api/models/app/agency/AgencyHome";
import { useChannelEvent } from "@/hooks/context/socket";
import { useUserProfile } from "@/hooks/context/user";
import { useAppStore } from "@/stores";
import useSwitchOrganization from "@/lib/api/hooks/app/organizations/useSwitchOrganization";
import { organizationsQuery } from "@/lib/api/hooks/app/organizations/useOrganizations";
import { createRefreshCoalescer } from "@/lib/realtime/refreshCoalescer";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

// Events for other client workspaces never reach this socket, so a slow poll
// while the page is visible covers them; the server caches the overview for 20s.
const OVERVIEW_POLL_MS = 60_000;

export const agencyOverviewQuery = (agencyId: string) =>
    queryOptions({
        queryKey: ["agency", agencyId, "overview"],
        queryFn: ({ signal }) => getAgencyOverview(agencyId, signal),
        staleTime: 15_000,
        refetchOnWindowFocus: true,
        refetchInterval: OVERVIEW_POLL_MS,
        refetchIntervalInBackground: false,
    });

export const emptyInboxFilters: AgencyInboxFilters = { clients: [], unread: false, needsReply: false, positive: false, q: "" };

export const agencyInboxQuery = (agencyId: string, filters: AgencyInboxFilters) =>
    infiniteQueryOptions<
        AgencyInboxPage,
        Error,
        InfiniteData<AgencyInboxPage, string | null>,
        ["agency", string, "inbox", AgencyInboxFilters],
        string | null
    >({
        queryKey: ["agency", agencyId, "inbox", filters],
        queryFn: ({ pageParam, signal }) => listAgencyInbox(agencyId, filters, pageParam ?? undefined, signal),
        initialPageParam: null,
        getNextPageParam: (last) => (last.pagination.has_more ? last.pagination.next_cursor : undefined),
        staleTime: 15_000,
        refetchOnWindowFocus: true,
        refetchInterval: OVERVIEW_POLL_MS,
        refetchIntervalInBackground: false,
    });

export function useAgencyOverview(agencyId?: string) {
    return useQuery({ ...agencyOverviewQuery(agencyId ?? ""), enabled: !!agencyId });
}

export function useAgencyInbox(agencyId: string | undefined, filters: AgencyInboxFilters) {
    return useInfiniteQuery({
        ...agencyInboxQuery(agencyId ?? "", filters),
        enabled: !!agencyId,
        placeholderData: (prev) => prev,
    });
}

export function useAgencyInboxThread(agencyId: string | undefined, organizationId: string | undefined, threadId: string | undefined) {
    return useQuery({
        queryKey: ["agency", agencyId, "inbox-thread", organizationId, threadId],
        queryFn: ({ signal }) => getAgencyInboxThread(agencyId!, organizationId!, threadId!, signal),
        enabled: !!agencyId && !!organizationId && !!threadId,
        staleTime: 15_000,
    });
}

export function useAgencyInboxMessage(agencyId: string | undefined, organizationId: string | undefined, messageId: string | undefined) {
    return useQuery({
        queryKey: ["agency", agencyId, "inbox-message", organizationId, messageId],
        queryFn: ({ signal }) => getAgencyInboxMessage(agencyId!, organizationId!, messageId!, signal),
        enabled: !!agencyId && !!organizationId && !!messageId,
        staleTime: 5 * 60_000,
    });
}

const INBOX_EVENTS = ["EMAIL_RECEIVED", "NEW_EMAIL", "INBOX_NEW", "EMAIL_UPDATED", "EMAIL_DELETED", "INBOX_UPDATE", "EMAIL_REPLIED"];

const isAgencyView = (agencyId: string, view: string) => (q: Query) =>
    q.queryKey[0] === "agency" && q.queryKey[1] === agencyId && q.queryKey[2] === view;

/**
 * Keeps the agency home and inbox live from what already reaches this socket:
 * the user channel (inbox mail of mailboxes the user connected, in any
 * workspace) and the current workspace's channel (its mail and its audit
 * spine). Other clients' events are not delivered here, which the queries'
 * focus refetch and slow poll cover.
 */
export function useAgencyLive(agencyId: string | undefined) {
    const qc = useQueryClient();
    const { user } = useUserProfile();
    const currentOrg = useAppStore((s) => s.currentOrganization);
    const [coalescer] = useState(() => createRefreshCoalescer(qc, { delayMs: 4000, maxWaitMs: 15_000 }));
    useEffect(() => () => coalescer.dispose(), [coalescer]);

    const onEvent = (payload: Record<string, unknown>) => {
        if (!agencyId) return;
        const event = String(payload.event_type ?? payload.type ?? payload._event ?? "")
            .replace(/[.:\s-]+/g, "_")
            .toUpperCase();
        if (!event || event.startsWith("PRESENCE") || event.startsWith("LIVE_") || event === "RATE_LIMITED") return;
        // Warmup traffic runs all day across the pool and moves nothing on these pages.
        if (event.includes("WARMUP") && !event.includes("HEALTH")) return;
        const inbox = INBOX_EVENTS.includes(event) || (event.includes("AUDIT") && payload.entity_type === "unibox");
        const key = ["agency", agencyId];
        coalescer.add([key], isAgencyView(agencyId, "overview"));
        if (inbox) coalescer.add([key], isAgencyView(agencyId, "inbox"));
    };

    useChannelEvent(user?.id ? `user:${user.id}` : "", "*", onEvent, [agencyId]);
    useChannelEvent(currentOrg?.id ? `org:${currentOrg.id}` : "", "*", onEvent, [agencyId]);
}

/** Switches the session into a client workspace, then runs `then` (a typed navigate). */
export function useOpenClient() {
    const navigate = useNavigate();
    const qc = useQueryClient();
    const switchOrg = useSwitchOrganization();
    const organizations = useAppStore((s) => s.organizations);
    const current = useAppStore((s) => s.currentOrganization);
    const setCurrentOrganization = useAppStore((s) => s.setCurrentOrganization);

    const open = async (orgId: string, then: (nav: typeof navigate) => unknown = (nav) => nav({ to: "/app/emails" })) => {
        try {
            if (current?.id !== orgId) {
                await switchOrg.mutateAsync(orgId);
                // A client added a moment ago may not be in the cached list yet.
                const org =
                    organizations.find((o) => o.id === orgId) ??
                    (await qc.fetchQuery({ ...organizationsQuery, staleTime: 0 })).find((o) => o.id === orgId);
                if (org) setCurrentOrganization(org);
            }
            await then(navigate);
            return true;
        } catch (e) {
            toast.error(buildError(e as AppError));
            return false;
        }
    };
    return { open, pending: switchOrg.isPending };
}
