import getSequences from "@/lib/api/client/app/campaigns/sequences/getSequences";
import { useSuspenseQuery } from "@tanstack/react-query";

const useSequences = (campaign_id: string) => useSuspenseQuery({
    queryKey: ["campaigns", campaign_id, "sequences"],
    queryFn: () => getSequences(campaign_id),
})

export default useSequences;
