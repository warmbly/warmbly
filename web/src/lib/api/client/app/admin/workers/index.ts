// All admin worker-management API calls. Each function is a thin axios wrapper
// over Request<T>; pages call these directly via react-query.

import Request from "../../../Request";
import type {
    CreateWorkerInput,
    CreateWorkerResponse,
    ManagedWorker,
    WorkerLiveStatus,
} from "../../../../models/app/admin/Worker";

export function listManagedWorkers(): Promise<{ data: ManagedWorker[] }> {
    return Request({
        method: "GET",
        url: "/admin/workers/managed",
        authorization: true,
    });
}

export function getManagedWorker(id: string): Promise<ManagedWorker> {
    return Request({
        method: "GET",
        url: `/admin/workers/${id}/managed`,
        authorization: true,
    });
}

export function createWorker(input: CreateWorkerInput): Promise<CreateWorkerResponse> {
    return Request({
        method: "POST",
        url: "/admin/workers",
        data: input,
        authorization: true,
    });
}

export function testWorker(id: string): Promise<{ ok: boolean; error?: string }> {
    return Request({
        method: "POST",
        url: `/admin/workers/${id}/test`,
        authorization: true,
    });
}

export function installWorker(id: string): Promise<{ ok: boolean }> {
    return Request({
        method: "POST",
        url: `/admin/workers/${id}/install`,
        authorization: true,
    });
}

export function restartWorker(id: string): Promise<{ ok: boolean }> {
    return Request({
        method: "POST",
        url: `/admin/workers/${id}/restart`,
        authorization: true,
    });
}

export function upgradeWorker(id: string): Promise<{ ok: boolean }> {
    return Request({
        method: "POST",
        url: `/admin/workers/${id}/upgrade`,
        authorization: true,
    });
}

export function uninstallWorker(id: string): Promise<{ ok: boolean }> {
    return Request({
        method: "POST",
        url: `/admin/workers/${id}/uninstall`,
        authorization: true,
    });
}

export function rotateWorkerKeys(id: string): Promise<{ ssh_public_key: string }> {
    return Request({
        method: "POST",
        url: `/admin/workers/${id}/rotate-keys`,
        authorization: true,
    });
}

export function getWorkerLiveStatus(id: string): Promise<WorkerLiveStatus> {
    return Request({
        method: "GET",
        url: `/admin/workers/${id}/live-status`,
        authorization: true,
    });
}

export function getWorkerLogs(id: string, lines = 200): Promise<{ logs: string }> {
    return Request({
        method: "GET",
        url: `/admin/workers/${id}/logs?lines=${lines}`,
        authorization: true,
    });
}

export function deleteWorker(id: string): Promise<{ ok: boolean }> {
    return Request({
        method: "DELETE",
        url: `/admin/workers/${id}`,
        authorization: true,
    });
}

export function systemUpdateWorker(id: string): Promise<{ output: string; reboot_required: boolean }> {
    return Request({
        method: "POST",
        url: `/admin/workers/${id}/system-update`,
        authorization: true,
    });
}

export function rebootWorker(id: string): Promise<{ ok: boolean }> {
    return Request({
        method: "POST",
        url: `/admin/workers/${id}/reboot`,
        authorization: true,
    });
}

export function setWorkerTags(workerID: string, tags: string[]): Promise<{ ok: boolean; tags: string[] }> {
    return Request({
        method: "PUT",
        url: `/admin/workers/${workerID}/tags`,
        data: { tags },
        authorization: true,
    });
}

export function listAllWorkerTags(): Promise<{ data: string[] }> {
    return Request({
        method: "GET",
        url: "/admin/workers/tags",
        authorization: true,
    });
}

export function preflightWorker(host: string, port: number): Promise<{ ok: boolean; latency_ms?: number; error?: string }> {
    return Request({
        method: "POST",
        url: "/admin/workers/preflight",
        data: { host, port },
        authorization: true,
    });
}

interface AdminUserSearchResult {
    data: { id: string; email: string; first_name?: string; last_name?: string }[];
    pagination?: { has_more?: boolean };
}

export function searchAdminUsers(query: string, limit = 10): Promise<AdminUserSearchResult> {
    const q = new URLSearchParams();
    if (query) q.set("query", query);
    q.set("limit", String(limit));
    return Request({
        method: "GET",
        url: "/admin/users?" + q.toString(),
        authorization: true,
    });
}

export function setWorkerRiskPool(workerID: string, pool: "clean" | "risky" | "quarantine"): Promise<{ ok: boolean }> {
    return Request({
        method: "PUT",
        url: `/admin/workers/${workerID}/risk-pool`,
        data: { risk_pool: pool },
        authorization: true,
    });
}

export function convertWorkerToDedicated(
    workerID: string,
    body: { user_id: string; subscription_id: string; drain_to_worker_id?: string | null },
): Promise<{ ok: boolean; accounts_drained: number; new_assignment: boolean }> {
    return Request({
        method: "POST",
        url: `/admin/workers/${workerID}/convert-to-dedicated`,
        data: body,
        authorization: true,
    });
}

// Reassign emails from one worker to another. The :id in the URL is the
// TARGET worker; the body lists the email-account IDs to move.
export function reassignEmailsToWorker(targetWorkerID: string, emailIDs: string[]): Promise<{ message: string }> {
    return Request({
        method: "POST",
        url: `/admin/workers/${targetWorkerID}/reassign`,
        data: { email_ids: emailIDs },
        authorization: true,
    });
}

// List email account IDs assigned to a worker. Used by the manual rewire flow.
export function listWorkerEmails(workerID: string, limit = 200): Promise<{ data: { id: string; email: string }[]; pagination: { has_more?: boolean } }> {
    return Request({
        method: "GET",
        url: `/admin/workers/${workerID}/emails?limit=${limit}`,
        authorization: true,
    });
}
