import Request from "../../Request";
import type { AgencyInboxFilters, AgencyInboxPage, AgencyOverview } from "@/lib/api/models/app/agency/AgencyHome";
import type UniboxThread from "@/lib/api/models/app/unibox/UniboxThread";
import type { UniboxEmailDetail } from "@/lib/api/models/app/unibox/UniboxEmail";

export const getAgencyOverview = (agencyId: string, signal?: AbortSignal) =>
    Request<AgencyOverview>({ method: "GET", url: `/agency/${agencyId}/overview`, authorization: true, signal });

export const listAgencyInbox = (agencyId: string, filters: AgencyInboxFilters, cursor?: string, signal?: AbortSignal) =>
    Request<AgencyInboxPage>({
        method: "GET",
        url: `/agency/${agencyId}/inbox`,
        params: {
            client: filters.clients.length ? filters.clients.join(",") : undefined,
            unread: filters.unread || undefined,
            needs_reply: filters.needsReply || undefined,
            positive: filters.positive || undefined,
            q: filters.q.trim() || undefined,
            cursor: cursor || undefined,
        },
        authorization: true,
        signal,
    });

export const getAgencyInboxThread = (agencyId: string, organizationId: string, threadId: string, signal?: AbortSignal) =>
    Request<UniboxThread>({
        method: "GET",
        url: `/agency/${agencyId}/inbox/thread`,
        params: { organization_id: organizationId, thread_id: threadId },
        authorization: true,
        signal,
    });

export const getAgencyInboxMessage = (agencyId: string, organizationId: string, messageId: string, signal?: AbortSignal) =>
    Request<UniboxEmailDetail>({
        method: "GET",
        url: `/agency/${agencyId}/inbox/messages/${messageId}`,
        params: { organization_id: organizationId },
        authorization: true,
        signal,
    });
