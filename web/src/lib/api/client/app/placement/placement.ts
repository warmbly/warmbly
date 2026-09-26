import type {
    CreatePlacementTestRequest,
    PlacementMonitor,
    PlacementMonitorInput,
    PlacementOverview,
    PlacementTest,
    PlacementTestDetail,
    PlacementTestList,
    PlacementWorkspaceSeed,
} from "@/lib/api/models/app/placement/Placement";
import Request from "../../Request";

export async function getPlacementOverview(): Promise<PlacementOverview> {
    const res = await Request<{ data: PlacementOverview }>({
        method: "GET",
        url: "/placement/overview",
        authorization: true,
    });
    return res.data;
}

export async function listPlacementTests(
    cursor: string | null,
    limit: number,
    campaignId?: string | null,
): Promise<PlacementTestList> {
    const params = new URLSearchParams();
    params.set("limit", String(limit));
    if (cursor) params.set("cursor", cursor);
    if (campaignId) params.set("campaign_id", campaignId);
    return await Request<PlacementTestList>({
        method: "GET",
        url: `/placement/tests?${params.toString()}`,
        authorization: true,
    });
}

export async function getPlacementTest(id: string): Promise<PlacementTestDetail> {
    const res = await Request<{ data: PlacementTestDetail }>({
        method: "GET",
        url: `/placement/tests/${id}`,
        authorization: true,
    });
    return res.data;
}

// One test, or two sharing a compare_group_id for a tracking comparison. The
// key makes a retried submit land on the tests the first attempt started.
export async function createPlacementTest(
    body: CreatePlacementTestRequest,
    idempotencyKey?: string,
): Promise<PlacementTest[]> {
    const res = await Request<{ data: PlacementTest[] | null }>({
        method: "POST",
        url: "/placement/tests",
        data: body,
        headers: idempotencyKey ? { "Idempotency-Key": idempotencyKey } : undefined,
        authorization: true,
    });
    return res.data ?? [];
}

// Stops the copies not sent yet; the sent ones keep being classified.
export async function cancelPlacementTest(id: string): Promise<PlacementTest> {
    const res = await Request<{ data: PlacementTest }>({
        method: "POST",
        url: `/placement/tests/${id}/cancel`,
        authorization: true,
    });
    return res.data;
}

export async function listPlacementSeeds(): Promise<PlacementWorkspaceSeed[]> {
    const res = await Request<{ data: PlacementWorkspaceSeed[] | null }>({
        method: "GET",
        url: "/placement/seeds",
        authorization: true,
    });
    return res.data ?? [];
}

export async function setPlacementSeed(emailAccountId: string, seed: boolean): Promise<PlacementWorkspaceSeed> {
    const res = await Request<{ data: PlacementWorkspaceSeed }>({
        method: "PUT",
        url: `/placement/seeds/${emailAccountId}`,
        data: { seed },
        authorization: true,
    });
    return res.data;
}

export async function getPlacementMonitor(campaignId: string): Promise<PlacementMonitor | null> {
    const res = await Request<{ data: PlacementMonitor | null }>({
        method: "GET",
        url: `/campaigns/${campaignId}/placement-monitor`,
        authorization: true,
    });
    return res.data ?? null;
}

// A full-state write, so a retry lands on the same monitor.
export async function putPlacementMonitor(campaignId: string, input: PlacementMonitorInput): Promise<PlacementMonitor> {
    const res = await Request<{ data: PlacementMonitor }>({
        method: "PUT",
        url: `/campaigns/${campaignId}/placement-monitor`,
        data: input,
        authorization: true,
    });
    return res.data;
}

export async function deletePlacementMonitor(campaignId: string): Promise<void> {
    await Request<void>({
        method: "DELETE",
        url: `/campaigns/${campaignId}/placement-monitor`,
        authorization: true,
    });
}
