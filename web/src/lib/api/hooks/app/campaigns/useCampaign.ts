import getCampaign from "@/lib/api/client/app/campaigns/getCampaign";
import { useSuspenseQuery } from "@tanstack/react-query";

const useCampaign = (id: string) => useSuspenseQuery({
    queryKey: ["campaigns", id],
    queryFn: () => getCampaign(id),
})

export default useCampaign;
