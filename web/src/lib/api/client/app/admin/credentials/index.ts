import Request from "../../../Request";
import type {
    AWSCredentials,
    AWSCredentialsBody,
    ApplyResult,
    ReleasesState,
    WorkerProfile,
    WorkerProfileBody,
} from "../../../../models/app/admin/Credentials";
import type { ManagedWorker } from "../../../../models/app/admin/Worker";

// AWS credentials

export function listAWSCredentials(): Promise<{ data: AWSCredentials[] }> {
    return Request({ method: "GET", url: "/admin/aws-credentials", authorization: true });
}
export function getAWSCredentials(id: string): Promise<AWSCredentials> {
    return Request({ method: "GET", url: `/admin/aws-credentials/${id}`, authorization: true });
}
export function createAWSCredentials(body: AWSCredentialsBody): Promise<{ id: string }> {
    return Request({ method: "POST", url: "/admin/aws-credentials", data: body, authorization: true });
}
export function updateAWSCredentials(id: string, body: AWSCredentialsBody): Promise<{ ok: boolean }> {
    return Request({ method: "PATCH", url: `/admin/aws-credentials/${id}`, data: body, authorization: true });
}
export function deleteAWSCredentials(id: string): Promise<{ ok: boolean }> {
    return Request({ method: "DELETE", url: `/admin/aws-credentials/${id}`, authorization: true });
}

// Worker profiles

export function listWorkerProfiles(): Promise<{ data: WorkerProfile[] }> {
    return Request({ method: "GET", url: "/admin/worker-profiles", authorization: true });
}
export function getWorkerProfile(id: string): Promise<WorkerProfile> {
    return Request({ method: "GET", url: `/admin/worker-profiles/${id}`, authorization: true });
}
export function createWorkerProfile(body: WorkerProfileBody): Promise<{ id: string }> {
    return Request({ method: "POST", url: "/admin/worker-profiles", data: body, authorization: true });
}
export function updateWorkerProfile(id: string, body: WorkerProfileBody): Promise<{ ok: boolean }> {
    return Request({ method: "PATCH", url: `/admin/worker-profiles/${id}`, data: body, authorization: true });
}
export function deleteWorkerProfile(id: string): Promise<{ ok: boolean }> {
    return Request({ method: "DELETE", url: `/admin/worker-profiles/${id}`, authorization: true });
}
export function listProfileWorkers(id: string): Promise<{ data: ManagedWorker[] }> {
    return Request({ method: "GET", url: `/admin/worker-profiles/${id}/workers`, authorization: true });
}
export function applyProfileToAll(id: string): Promise<ApplyResult> {
    return Request({ method: "POST", url: `/admin/worker-profiles/${id}/apply`, authorization: true });
}

// Worker assignment + single apply

export function assignWorkerProfile(workerID: string, profileID: string | null): Promise<{ ok: boolean }> {
    return Request({
        method: "PUT",
        url: `/admin/workers/${workerID}/profile`,
        data: { profile_id: profileID },
        authorization: true,
    });
}

export function applyWorkerConfig(workerID: string): Promise<{ ok: boolean }> {
    return Request({ method: "POST", url: `/admin/workers/${workerID}/apply`, authorization: true });
}

// Releases

export function getReleasesState(): Promise<ReleasesState> {
    return Request({ method: "GET", url: "/admin/releases/state", authorization: true });
}

export function checkReleases(): Promise<{ state: ReleasesState; changed: unknown[] }> {
    return Request({ method: "POST", url: "/admin/releases/check", authorization: true });
}

export function setProfileRelease(profileID: string, channel: "pinned" | "stable" | "dev", autoUpdate: boolean): Promise<{ ok: boolean }> {
    return Request({
        method: "PUT",
        url: `/admin/worker-profiles/${profileID}/release`,
        data: { channel, auto_update: autoUpdate },
        authorization: true,
    });
}
