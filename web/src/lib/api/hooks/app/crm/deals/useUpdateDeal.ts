import { useMutation, useQueryClient } from "@tanstack/react-query";
import type Deal from "@/lib/api/models/app/crm/Deal";
import updateDeal from "@/lib/api/client/app/crm/deals/updateDeal";

export default function useUpdateDeal() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: ({ id, data }: { id: string; data: Partial<Deal> }) => updateDeal(id, data),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["crm", "deals"],
            })
            // Deals also render inside contacts (panel list + 360 timeline), so
            // a stage move / edit must refresh any active contact queries too.
            queryClient.invalidateQueries({
                queryKey: ["contacts"],
            })
        }
    })
}
