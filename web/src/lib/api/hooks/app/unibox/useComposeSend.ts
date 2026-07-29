import { useMutation, useQueryClient } from "@tanstack/react-query";
import composeSend from "@/lib/api/client/app/unibox/composeSend";
import type { ComposeSendInput } from "@/lib/api/models/app/unibox/Compose";

// Sends (or schedules) a fresh outbound email. Invalidating the whole
// ["unibox"] tree refreshes the list, overview counts, and scheduled queue.
export default function useComposeSend() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (data: ComposeSendInput) => composeSend(data),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["unibox"],
            })
        }
    })
}
