import deleteCampaign from "@/lib/api/client/app/campaigns/deleteCampaign";
import type GetCampaigns from "@/lib/api/models/app/campaigns/GetCampaigns";
import { type InfiniteData, useMutation, useQueryClient } from "@tanstack/react-query";

export default function useDeleteCampaign(id: string) {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: async () =>
            deleteCampaign(id),
        onSuccess: () => {
            const allLists = queryClient.getQueriesData<InfiniteData<GetCampaigns>>({
                queryKey: ["campaigns", "list"],
            });

            for (const [key, oldData] of allLists) {
                if (!oldData) continue;

                queryClient.setQueryData(key, {
                    ...oldData,
                    pages: oldData.pages.map((page) => ({
                        ...page,
                        data: page.data.filter((c) => c.id !== id),
                    })),
                });
            }

            queryClient.invalidateQueries({
                queryKey: ["campaigns", id]
            });
        }
    })
}
