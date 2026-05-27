// /admin/provisioning-templates and /admin/provisioning-jobs.
//
// Templates store the full Hetzner provisioning config so an operator
// can re-launch the same kind of worker box without re-entering every
// field. Jobs are the runtime side: one provisioning request, tracked
// through the create-server → install → verify state machine.
//
// Every list/get call tolerates 404/501 with an empty result so the UI
// can render a "Backend endpoint not yet available" placeholder while
// the API team wires the routes.

import { Request, APIError } from "@/lib/api/client";
import type {
    ProvisioningJob,
    ProvisioningJobCreate,
    ProvisioningTemplate,
    ProvisioningTemplateCreate,
    WorkerProfile,
} from "@/lib/api/models/admin";

function isNotReady(err: unknown): boolean {
    return err instanceof APIError && (err.status === 404 || err.status === 501);
}

// --------------------------------------------------------------------
// Templates
// --------------------------------------------------------------------

export async function listProvisioningTemplates(): Promise<ProvisioningTemplate[]> {
    try {
        const res = await Request<
            { data: ProvisioningTemplate[] } | ProvisioningTemplate[]
        >({
            method: "GET",
            url: "/admin/provisioning-templates",
            authorization: true,
        });
        return Array.isArray(res) ? res : res.data ?? [];
    } catch (err) {
        if (isNotReady(err)) return [];
        throw err;
    }
}

export async function getProvisioningTemplate(
    id: string,
): Promise<ProvisioningTemplate | null> {
    try {
        return await Request<ProvisioningTemplate>({
            method: "GET",
            url: `/admin/provisioning-templates/${id}`,
            authorization: true,
        });
    } catch (err) {
        if (isNotReady(err)) return null;
        throw err;
    }
}

export function createProvisioningTemplate(
    body: ProvisioningTemplateCreate,
): Promise<ProvisioningTemplate> {
    return Request({
        method: "POST",
        url: "/admin/provisioning-templates",
        data: body,
        authorization: true,
    });
}

export function updateProvisioningTemplate(
    id: string,
    body: ProvisioningTemplateCreate,
): Promise<ProvisioningTemplate> {
    return Request({
        method: "PUT",
        url: `/admin/provisioning-templates/${id}`,
        data: body,
        authorization: true,
    });
}

export function deleteProvisioningTemplate(
    id: string,
): Promise<{ ok: boolean }> {
    return Request({
        method: "DELETE",
        url: `/admin/provisioning-templates/${id}`,
        authorization: true,
    });
}

// --------------------------------------------------------------------
// Jobs
// --------------------------------------------------------------------

export interface ListProvisioningJobsParams {
    state?: string;
    provider?: string;
    /** Limit by created_at >= now - days. */
    since_days?: number;
}

export async function listProvisioningJobs(
    params: ListProvisioningJobsParams = {},
): Promise<ProvisioningJob[]> {
    try {
        const search = new URLSearchParams();
        if (params.state) search.set("state", params.state);
        if (params.provider) search.set("provider", params.provider);
        if (params.since_days) search.set("since_days", String(params.since_days));
        const qs = search.toString();
        const res = await Request<
            { data: ProvisioningJob[] } | ProvisioningJob[]
        >({
            method: "GET",
            url: `/admin/provisioning-jobs${qs ? `?${qs}` : ""}`,
            authorization: true,
        });
        return Array.isArray(res) ? res : res.data ?? [];
    } catch (err) {
        if (isNotReady(err)) return [];
        throw err;
    }
}

export async function getProvisioningJob(
    id: string,
): Promise<ProvisioningJob | null> {
    try {
        return await Request<ProvisioningJob>({
            method: "GET",
            url: `/admin/provisioning-jobs/${id}`,
            authorization: true,
        });
    } catch (err) {
        if (isNotReady(err)) return null;
        throw err;
    }
}

export function createProvisioningJob(
    body: ProvisioningJobCreate,
): Promise<ProvisioningJob> {
    return Request({
        method: "POST",
        url: "/admin/provisioning-jobs",
        data: body,
        authorization: true,
    });
}

export function retryProvisioningJob(id: string): Promise<ProvisioningJob> {
    return Request({
        method: "POST",
        url: `/admin/provisioning-jobs/${id}/retry`,
        authorization: true,
    });
}

// --------------------------------------------------------------------
// Worker profiles (read-only)
// --------------------------------------------------------------------

export async function listWorkerProfiles(): Promise<WorkerProfile[]> {
    try {
        const res = await Request<{ data: WorkerProfile[] } | WorkerProfile[]>({
            method: "GET",
            url: "/admin/worker-profiles",
            authorization: true,
        });
        return Array.isArray(res) ? res : res.data ?? [];
    } catch (err) {
        if (isNotReady(err)) return [];
        throw err;
    }
}
