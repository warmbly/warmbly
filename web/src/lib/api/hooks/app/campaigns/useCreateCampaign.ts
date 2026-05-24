import createCampaign from "@/lib/api/client/app/campaigns/createCampaign";
import { useMutation, useQueryClient } from "@tanstack/react-query";

interface CreateCampaignParams {
    name: string;
    description: string;
}

export default function useCreateCampaign() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: async ({ name, description }: CreateCampaignParams) =>
            createCampaign(name, description),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["campaigns", "list"],
            })
        }
    })
}
