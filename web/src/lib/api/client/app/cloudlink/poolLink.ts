// /pool-link/* — the cloud side: approving a self-hosted instance's code and
// managing the workspace's linked instances.

import Request from "@/lib/api/client/Request";
import type { PoolLinkInstance, PoolLinkPlan } from "@/lib/api/models/app/cloudlink/CloudLink";

export async function listPoolLinkInstances(): Promise<{ data: PoolLinkInstance[]; plan: PoolLinkPlan }> {
    return await Request<{ data: PoolLinkInstance[]; plan: PoolLinkPlan }>({ method: "GET", url: "/pool-link/instances", authorization: true });
}

export async function revokePoolLinkInstance(id: string): Promise<void> {
    await Request<void>({ method: "DELETE", url: `/pool-link/instances/${id}`, authorization: true });
}

