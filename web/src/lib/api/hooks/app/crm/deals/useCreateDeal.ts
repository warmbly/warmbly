import { useMutation, useQueryClient } from "@tanstack/react-query";
import type Deal from "@/lib/api/models/app/crm/Deal";
import createDeal from "@/lib/api/client/app/crm/deals/createDeal";

export default function useCreateDeal() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (data: Partial<Deal>) => createDeal(data),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["crm", "deals", "list"],
            })
        }
    })
}
