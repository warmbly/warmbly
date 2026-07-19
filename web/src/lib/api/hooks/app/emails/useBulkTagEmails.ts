import bulkTagEmails from "@/lib/api/client/app/emails/bulkTagEmails";
import { useMutation, useQueryClient } from "@tanstack/react-query";

export default function useBulkTagEmails() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: ({ emailIds, addTags, removeTags }: { emailIds: string[]; addTags: string[]; removeTags: string[] }) =>
            bulkTagEmails(emailIds, addTags, removeTags),
        onSuccess: () => {
            // Tag membership changed on many rows; refetch lists and details.
            queryClient.invalidateQueries({ queryKey: ["emails"] });
        }
    })
}
