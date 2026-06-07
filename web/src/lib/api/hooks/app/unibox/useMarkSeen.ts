import { useMutation, useQueryClient } from "@tanstack/react-query";
import markSeen from "@/lib/api/client/app/unibox/markSeen";

export default function useMarkSeen() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (data: { ids: string[]; seen?: boolean }) => markSeen(data),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["unibox"],
            })
        }
    })
}
