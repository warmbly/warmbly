// /admin/cloud-credentials + /admin/cloud-providers/* — Hetzner Cloud API
// token storage, connection-test, and provider catalog endpoints.
//
// All endpoints below are still being wired by the API team. Each call
// tolerates 404/501 by returning a typed empty result so the UI can
// render a "Backend endpoint not yet available" placeholder.

import { Request, APIError } from "@/lib/api/client";
import type {
    CloudCredential,
    CloudCredentialCreate,
    CloudCredentialTestResult,
    HetznerLocation,
    HetznerServerType,
} from "@/lib/api/models/admin";

export class EndpointNotReadyError extends Error {
    constructor(public endpoint: string) {
        super(`Endpoint not available: ${endpoint}`);
        this.name = "EndpointNotReadyError";
    }
}

function isNotReady(err: unknown): boolean {
    return err instanceof APIError && (err.status === 404 || err.status === 501);
}

export async function listCloudCredentials(): Promise<CloudCredential[]> {
    try {
        const res = await Request<{ data: CloudCredential[] } | CloudCredential[]>({
            method: "GET",
            url: "/admin/cloud-credentials",
            authorization: true,
        });
        return Array.isArray(res) ? res : res.data ?? [];
    } catch (err) {
        if (isNotReady(err)) return [];
        throw err;
    }
}

export function createCloudCredential(
    body: CloudCredentialCreate,
): Promise<CloudCredential> {
    return Request({
        method: "POST",
        url: "/admin/cloud-credentials",
        data: body,
        authorization: true,
    });
}

export function updateCloudCredential(
    id: string,
    body: Partial<CloudCredentialCreate>,
): Promise<CloudCredential> {
    return Request({
        method: "PUT",
        url: `/admin/cloud-credentials/${id}`,
        data: body,
        authorization: true,
    });
}

export function deleteCloudCredential(id: string): Promise<{ ok: boolean }> {
    return Request({
        method: "DELETE",
        url: `/admin/cloud-credentials/${id}`,
        authorization: true,
    });
}

export async function testHetznerCredential(
    credentialId?: string,
): Promise<CloudCredentialTestResult> {
    try {
        return await Request<CloudCredentialTestResult>({
            method: "POST",
            url: "/admin/cloud-providers/hetzner/test",
            data: credentialId ? { credential_id: credentialId } : undefined,
            authorization: true,
        });
    } catch (err) {
        if (isNotReady(err)) {
            return {
                ok: false,
                error: "Backend test endpoint not yet available.",
            };
        }
        if (err instanceof APIError) {
            return { ok: false, error: err.message };
        }
        throw err;
    }
}

// --------------------------------------------------------------------
// Provider catalog
// --------------------------------------------------------------------

export async function listHetznerLocations(): Promise<HetznerLocation[]> {
    try {
        const res = await Request<{ data: HetznerLocation[] } | HetznerLocation[]>({
            method: "GET",
            url: "/admin/cloud-providers/hetzner/locations",
            authorization: true,
        });
        return Array.isArray(res) ? res : res.data ?? [];
    } catch (err) {
        if (isNotReady(err)) return [];
        throw err;
    }
}

export async function listHetznerServerTypes(): Promise<HetznerServerType[]> {
    try {
        const res = await Request<
            { data: HetznerServerType[] } | HetznerServerType[]
        >({
            method: "GET",
            url: "/admin/cloud-providers/hetzner/server-types",
            authorization: true,
        });
        return Array.isArray(res) ? res : res.data ?? [];
    } catch (err) {
        if (isNotReady(err)) return [];
        throw err;
    }
}
