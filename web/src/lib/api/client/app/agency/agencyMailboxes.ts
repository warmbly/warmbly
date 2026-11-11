import Request from "../../Request";
import type {
    AgencyMailboxFilters,
    AgencyMailboxPage,
    AgencyVendorAccount,
    AgencyVendorAccountInput,
    AgencyVendorAccountList,
} from "@/lib/api/models/app/agency/AgencyMailboxes";
import type { VendorConnection } from "@/lib/api/models/app/emails/MailboxSources";

export const listAgencyMailboxes = (agencyId: string, filters: AgencyMailboxFilters, cursor?: string, signal?: AbortSignal) =>
    Request<AgencyMailboxPage>({
        method: "GET",
        url: `/agency/${agencyId}/mailboxes`,
        params: {
            client: filters.clients.length ? filters.clients.join(",") : undefined,
            attention: filters.attention || undefined,
            q: filters.q.trim() || undefined,
            cursor: cursor || undefined,
        },
        authorization: true,
        signal,
    });

export const listAgencyVendors = (agencyId: string, signal?: AbortSignal) =>
    Request<AgencyVendorAccountList>({ method: "GET", url: `/agency/${agencyId}/vendors`, authorization: true, signal });

export const createAgencyVendor = (agencyId: string, data: AgencyVendorAccountInput) =>
    Request<AgencyVendorAccount>({ method: "POST", url: `/agency/${agencyId}/vendors`, data, authorization: true });

export const updateAgencyVendor = (agencyId: string, id: string, data: AgencyVendorAccountInput) =>
    Request<AgencyVendorAccount>({ method: "PATCH", url: `/agency/${agencyId}/vendors/${id}`, data, authorization: true });

export const deleteAgencyVendor = (agencyId: string, id: string) =>
    Request<void>({ method: "DELETE", url: `/agency/${agencyId}/vendors/${id}`, authorization: true });

/** The client workspace's connection to the account, made the first time it is asked for. */
export const putAgencyVendorInClient = (agencyId: string, id: string, orgId: string) =>
    Request<VendorConnection>({ method: "PUT", url: `/agency/${agencyId}/vendors/${id}/clients/${orgId}`, authorization: true });
