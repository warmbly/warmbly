import { useQuery } from "@tanstack/react-query";
import getCampaignLogs from "@/lib/api/client/app/campaigns/getCampaignLogs";

export default function useCampaignLogs(id: string) {
    return useQuery({
        queryKey: ["campaigns", id, "logs"],
        queryFn: () => getCampaignLogs(id),
        enabled: !!id,
    })
}
