// /pool-link/* — the cloud side: approving a self-hosted instance's code and
// managing the workspace's linked instances.

import Request from "@/lib/api/client/Request";
import type { PoolLinkCode, PoolLinkInstance, PoolLinkOffer, PoolLinkPlan } from "@/lib/api/models/app/cloudlink/CloudLink";

export async function describePoolLinkCode(code: string): Promise<PoolLinkCode> {
    return await Request<PoolLinkCode>({ method: "GET", url: `/pool-link/codes/${encodeURIComponent(code)}`, authorization: true });
}

export async function approvePoolLinkCode(code: string, organizationId: string): Promise<PoolLinkInstance> {
    return await Request<PoolLinkInstance>({
        method: "POST",
        url: `/pool-link/codes/${encodeURIComponent(code)}/approve`,
        data: { organization_id: organizationId },
        authorization: true,
    });
}

export async function denyPoolLinkCode(code: string): Promise<void> {
    await Request<void>({ method: "POST", url: `/pool-link/codes/${encodeURIComponent(code)}/deny`, authorization: true });
}

export async function listPoolLinkInstances(): Promise<{ data: PoolLinkInstance[]; plan: PoolLinkPlan }> {
    return await Request<{ data: PoolLinkInstance[]; plan: PoolLinkPlan }>({ method: "GET", url: "/pool-link/instances", authorization: true });
}

export async function revokePoolLinkInstance(id: string): Promise<void> {
    await Request<void>({ method: "DELETE", url: `/pool-link/instances/${id}`, authorization: true });
}

export async function getPoolLinkOffer(): Promise<PoolLinkOffer> {
    return await Request<PoolLinkOffer>({ method: "GET", url: "/pool-link/offer", authorization: true });
}

export async function startPoolLinkCheckout(interval: "month" | "year"): Promise<{ checkout_url: string }> {
    return await Request<{ checkout_url: string }>({
        method: "POST",
        url: "/pool-link/checkout",
        data: { interval },
        authorization: true,
    });
}
