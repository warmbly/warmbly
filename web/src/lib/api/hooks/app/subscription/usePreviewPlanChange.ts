import { useQuery } from "@tanstack/react-query";
import previewPlanChange from "@/lib/api/client/app/subscription/previewPlanChange";

export default function usePreviewPlanChange(planId: string) {
    return useQuery({
        queryKey: ["subscription", "preview-change", planId],
        queryFn: () => previewPlanChange(planId),
        enabled: !!planId,
    })
}
