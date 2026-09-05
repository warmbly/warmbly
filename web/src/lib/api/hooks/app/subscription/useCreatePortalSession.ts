import { useMutation } from "@tanstack/react-query";
import createPortalSession, {
    type CreatePortalInput,
} from "@/lib/api/client/app/subscription/createPortalSession";

export default function useCreatePortalSession() {
    return useMutation({
        mutationFn: (data: CreatePortalInput) => createPortalSession(data),
    })
}
