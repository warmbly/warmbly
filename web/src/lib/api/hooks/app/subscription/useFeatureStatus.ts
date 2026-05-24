import { useQuery } from "@tanstack/react-query";
import getFeatureStatus from "@/lib/api/client/app/subscription/getFeatureStatus";

export default function useFeatureStatus() {
    return useQuery({
        queryKey: ["subscription", "features"],
        queryFn: () => getFeatureStatus(),
    })
}
