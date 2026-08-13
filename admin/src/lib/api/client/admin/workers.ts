// /admin/workers/* — the SSH-managed-worker control plane.

import { Request } from "@/lib/api/client";
import type {
    AdminWorkerEmailsResult,
    CreateWorkerInput,
    CreateWorkerResponse,
    ManagedWorker,
    WorkerLiveStatus,
} from "@/lib/api/models/admin";

// getWorkerEmails returns the mailboxes assigned to a worker (paginated), with
// per-mailbox risk band + warmup health so the detail page can show how healthy
// the inboxes on this worker are.
export function getWorkerEmails(
    id: string,
    cursor?: string,
): Promise<AdminWorkerEmailsResult> {
    const q = cursor ? `?cursor=${cursor}` : "";
    return Request({
        method: "GET",
        url: `/admin/workers/${id}/emails${q}`,
        authorization: true,
    });
}

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

// Reachability probe before the row is created, so a typo'd host fails fast
// instead of after the keypair is minted.
export function preflightWorker(
    host: string,
    port: number,
): Promise<{ ok: boolean; latency_ms?: number; error?: string }> {
    return Request({
        method: "POST",
        url: "/admin/workers/preflight",
        data: { host, port },
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

export function uninstallWorker(id: string): Promise<{ ok: boolean }> {
    return Request({
        method: "POST",
        url: `/admin/workers/${id}/uninstall`,
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

// Pulls the newest image and restarts the unit. "Apply config" below only
// rewrites the env file, so this is the only path that changes the image.
export function upgradeWorker(id: string): Promise<{ ok: boolean }> {
    return Request({
        method: "POST",
        url: `/admin/workers/${id}/upgrade`,
        authorization: true,
    });
}

// Rewrites /etc/warmbly/worker.env over SSH and restarts, without pulling.
export function applyWorkerConfig(id: string): Promise<{ ok: boolean }> {
    return Request({
        method: "POST",
        url: `/admin/workers/${id}/apply`,
        authorization: true,
    });
}

// Mints a fresh keypair and returns the new public key, which has to be pasted
// into the VPS before anything else will authenticate again.
export function rotateWorkerKeys(id: string): Promise<{ ssh_public_key: string }> {
    return Request({
        method: "POST",
        url: `/admin/workers/${id}/rotate-keys`,
        authorization: true,
    });
}

export function systemUpdateWorker(
    id: string,
): Promise<{ output: string; reboot_required: boolean }> {
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

export function deleteWorker(id: string): Promise<{ ok: boolean }> {
    return Request({
        method: "DELETE",
        url: `/admin/workers/${id}`,
        authorization: true,
    });
}

export function setWorkerTags(
    workerID: string,
    tags: string[],
): Promise<{ ok: boolean; tags: string[] }> {
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
