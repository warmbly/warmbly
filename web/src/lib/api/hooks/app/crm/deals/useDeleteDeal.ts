import { useMutation, useQueryClient } from "@tanstack/react-query";
import deleteDeal from "@/lib/api/client/app/crm/deals/deleteDeal";

export default function useDeleteDeal() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (id: string) => deleteDeal(id),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["crm", "deals"],
            })
            // Deals also render inside contacts (panel list + 360 timeline).
            queryClient.invalidateQueries({
                queryKey: ["contacts"],
            })
        }
    })
}
