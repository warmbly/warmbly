import { useMutation } from "@tanstack/react-query";
import createPortalSession from "@/lib/api/client/app/subscription/createPortalSession";

export default function useCreatePortalSession() {
    return useMutation({
        mutationFn: () => createPortalSession(),
    })
}
