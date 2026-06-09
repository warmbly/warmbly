import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
    listABVariants,
    createABVariant,
    updateABVariant,
    deleteABVariant,
    getABAnalysis,
} from "@/lib/api/client/app/campaigns/abVariants";
import type {
    CreateABVariantInput,
    UpdateABVariantInput,
} from "@/lib/api/models/app/campaigns/ABVariant";

const key = (campaignId: string) => ["campaigns", campaignId, "ab-variants"];
const analysisKey = (campaignId: string) => ["campaigns", campaignId, "ab-analysis"];

function invalidateAB(qc: ReturnType<typeof useQueryClient>, campaignId: string) {
    qc.invalidateQueries({ queryKey: key(campaignId) });
    qc.invalidateQueries({ queryKey: analysisKey(campaignId) });
}

export function useCampaignABVariants(campaignId: string) {
    return useQuery({
        queryKey: key(campaignId),
        queryFn: () => listABVariants(campaignId),
        enabled: !!campaignId,
    });
}

export function useCampaignABAnalysis(campaignId: string, enabled = true) {
    return useQuery({
        queryKey: analysisKey(campaignId),
        queryFn: () => getABAnalysis(campaignId),
        enabled: enabled && !!campaignId,
        staleTime: 30_000,
    });
}

export function useCreateABVariant(campaignId: string) {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (input: CreateABVariantInput) => createABVariant(campaignId, input),
        onSuccess: () => invalidateAB(qc, campaignId),
    });
}

export function useUpdateABVariant(campaignId: string) {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ variantId, input }: { variantId: string; input: UpdateABVariantInput }) =>
            updateABVariant(campaignId, variantId, input),
        onSuccess: () => invalidateAB(qc, campaignId),
    });
}

export function useDeleteABVariant(campaignId: string) {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (variantId: string) => deleteABVariant(campaignId, variantId),
        onSuccess: () => invalidateAB(qc, campaignId),
    });
}
