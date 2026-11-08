import { useMutation, useQueryClient } from "@tanstack/react-query";
import leaveAgency from "@/lib/api/client/app/organizations/leaveAgency";

export default function useLeaveAgency() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: () => leaveAgency(),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["organizations"] });
            queryClient.invalidateQueries({ queryKey: ["subscription"] });
        },
    });
}
