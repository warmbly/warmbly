import type ABVariant from "@/lib/api/models/app/campaigns/ABVariant";
import type { ABWinnerAnalysis, CreateABVariantInput, UpdateABVariantInput } from "@/lib/api/models/app/campaigns/ABVariant";
import Request from "../../Request";

export async function listABVariants(campaignId: string): Promise<ABVariant[]> {
    const res = await Request<{ data: ABVariant[] }>({
        method: "GET",
        url: `/campaigns/${campaignId}/ab-variants`,
        authorization: true,
    });
    return res.data ?? [];
}

export async function createABVariant(campaignId: string, input: CreateABVariantInput): Promise<ABVariant> {
    return await Request<ABVariant>({
        method: "POST",
        url: `/campaigns/${campaignId}/ab-variants`,
        data: input,
        authorization: true,
    });
}

export async function updateABVariant(
    campaignId: string,
    variantId: string,
    input: UpdateABVariantInput,
): Promise<ABVariant> {
    return await Request<ABVariant>({
        method: "PATCH",
        url: `/campaigns/${campaignId}/ab-variants/${variantId}`,
        data: input,
        authorization: true,
    });
}

export async function deleteABVariant(campaignId: string, variantId: string): Promise<void> {
    await Request<void>({
        method: "DELETE",
        url: `/campaigns/${campaignId}/ab-variants/${variantId}`,
        authorization: true,
    });
}

// Per-variant performance + winner (bare object, no {data} envelope).
export async function getABAnalysis(campaignId: string): Promise<ABWinnerAnalysis> {
    return await Request<ABWinnerAnalysis>({
        method: "GET",
        url: `/campaigns/${campaignId}/ab-analysis`,
        authorization: true,
    });
}
