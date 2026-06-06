import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
    listABVariants,
    createABVariant,
    updateABVariant,
    deleteABVariant,
} from "@/lib/api/client/app/campaigns/abVariants";
import type {
    CreateABVariantInput,
    UpdateABVariantInput,
} from "@/lib/api/models/app/campaigns/ABVariant";

const key = (campaignId: string) => ["campaigns", campaignId, "ab-variants"];

export function useCampaignABVariants(campaignId: string) {
    return useQuery({
        queryKey: key(campaignId),
        queryFn: () => listABVariants(campaignId),
        enabled: !!campaignId,
    });
}

export function useCreateABVariant(campaignId: string) {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (input: CreateABVariantInput) => createABVariant(campaignId, input),
        onSuccess: () => qc.invalidateQueries({ queryKey: key(campaignId) }),
    });
}

export function useUpdateABVariant(campaignId: string) {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ variantId, input }: { variantId: string; input: UpdateABVariantInput }) =>
            updateABVariant(campaignId, variantId, input),
        onSuccess: () => qc.invalidateQueries({ queryKey: key(campaignId) }),
    });
}

export function useDeleteABVariant(campaignId: string) {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (variantId: string) => deleteABVariant(campaignId, variantId),
        onSuccess: () => qc.invalidateQueries({ queryKey: key(campaignId) }),
    });
}
