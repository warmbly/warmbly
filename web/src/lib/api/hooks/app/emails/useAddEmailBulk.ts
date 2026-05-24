import addEmailBulk from "@/lib/api/client/app/emails/addEmailBulk";
import type AddEmail from "@/lib/api/models/app/emails/AddEmail";
import { useMutation, useQueryClient } from "@tanstack/react-query";

export default function useAddEmailBulk() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (emails: AddEmail[]) => addEmailBulk(emails),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["emails", "list"]
            })
        }
    })
}
