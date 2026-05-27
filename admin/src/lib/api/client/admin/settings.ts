// /admin/settings/backends — pluggable infrastructure registry
// (KMS / blob storage / encrypted-keys / eventbus / cache / transport).
//
// The backend exposes three operations today:
//   GET    /admin/settings/backends
//   GET    /admin/settings/backends/active/:kind
//   POST   /admin/settings/backends/:id/activate
//
// The "list filtered by kind" variant the UI uses is implemented
// client-side over the full list, since the backend's listing endpoint
// doesn't take a `kind` query param yet. When it does, swap the impl.

import { Request, APIError } from "@/lib/api/client";
import type { StorageBackend, StorageBackendKind } from "@/lib/api/models/admin";

export async function listStorageBackends(
    kind?: StorageBackendKind,
): Promise<StorageBackend[]> {
    try {
        const res = await Request<{ data: StorageBackend[] } | StorageBackend[]>({
            method: "GET",
            url: "/admin/settings/backends",
            authorization: true,
        });
        const all = Array.isArray(res) ? res : res.data ?? [];
        return kind ? all.filter((b) => b.kind === kind) : all;
    } catch (err) {
        // Endpoint may not be wired in every environment yet — surface
        // a typed "missing" so the UI can render a placeholder card.
        if (err instanceof APIError && (err.status === 404 || err.status === 501)) {
            return [];
        }
        throw err;
    }
}

export async function getActiveBackend(
    kind: StorageBackendKind,
): Promise<StorageBackend | null> {
    try {
        return await Request<StorageBackend>({
            method: "GET",
            url: `/admin/settings/backends/active/${kind}`,
            authorization: true,
        });
    } catch (err) {
        if (err instanceof APIError && (err.status === 404 || err.status === 501)) {
            return null;
        }
        throw err;
    }
}

export function activateBackend(id: string): Promise<{ ok: boolean }> {
    return Request({
        method: "POST",
        url: `/admin/settings/backends/${id}/activate`,
        authorization: true,
    });
}
