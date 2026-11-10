import Request from "../../Request";
import type {
    AgencyAPIKey,
    AgencyAPIKeyPermissions,
    AgencyAPIKeysPage,
    AgencyAPIKeyWithSecret,
    CreateAgencyAPIKey,
    UpdateAgencyAPIKey,
} from "@/lib/api/models/app/agency/AgencyAPIKey";

export const listAgencyAPIKeys = (agencyId: string, cursor?: string) =>
    Request<AgencyAPIKeysPage>({
        method: "GET",
        url: `/agency/${agencyId}/api-keys${cursor ? `?cursor=${encodeURIComponent(cursor)}` : ""}`,
        authorization: true,
    });

export const listAgencyAPIKeyPermissions = (agencyId: string) =>
    Request<AgencyAPIKeyPermissions>({ method: "GET", url: `/agency/${agencyId}/api-keys/permissions`, authorization: true });

export const createAgencyAPIKey = (agencyId: string, data: CreateAgencyAPIKey) =>
    Request<AgencyAPIKeyWithSecret>({
        method: "POST",
        url: `/agency/${agencyId}/api-keys`,
        data,
        headers: { "Idempotency-Key": crypto.randomUUID() },
        authorization: true,
    });

export const updateAgencyAPIKey = (agencyId: string, keyId: string, data: UpdateAgencyAPIKey) =>
    Request<AgencyAPIKey>({ method: "PATCH", url: `/agency/${agencyId}/api-keys/${keyId}`, data, authorization: true });

export const revokeAgencyAPIKey = (agencyId: string, keyId: string) =>
    Request<void>({ method: "DELETE", url: `/agency/${agencyId}/api-keys/${keyId}`, authorization: true });
