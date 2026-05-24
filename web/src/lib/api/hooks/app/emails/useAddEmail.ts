import addEmail from "@/lib/api/client/app/emails/addEmail";
import type AddEmail from "@/lib/api/models/app/emails/AddEmail";
import { useMutation, useQueryClient } from "@tanstack/react-query";

export default function useAddEmail() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (email: AddEmail) => addEmail(email),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["emails", "list"]
            })
        }
    })
}
