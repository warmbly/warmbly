import { useMutation, useQueryClient, type InfiniteData } from "@tanstack/react-query";
import type Campaign from "@/lib/api/models/app/campaigns/Campaign";
import updateCampaign from "@/lib/api/client/app/campaigns/updateCampaign";
import type GetCampaigns from "@/lib/api/models/app/campaigns/GetCampaigns";

export default function useUpdateCampaign(id: string) {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (campaign: Partial<Campaign>) =>
            updateCampaign(id, campaign),
        onSuccess: (data) => {
            const allLists = queryClient.getQueriesData<InfiniteData<GetCampaigns>>({
                queryKey: ["campaigns", "list"],
            });

            for (const [key, oldData] of allLists) {
                if (!oldData) continue;

                queryClient.setQueryData(key, {
                    ...oldData,
                    pages: oldData.pages.map((page) => ({
                        ...page,
                        data: page.data.map((c) => c.id === id ? data : c),
                    })),
                });
            }

            queryClient.setQueryData<Campaign>(
                ["campaigns", id],
                data
            );
        }
    })
}
