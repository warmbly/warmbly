import { useQuery } from "@tanstack/react-query";
import getTrialStatus from "@/lib/api/client/app/subscription/getTrialStatus";

export default function useTrialStatus() {
    return useQuery({
        queryKey: ["subscription", "trial"],
        queryFn: () => getTrialStatus(),
    })
}
