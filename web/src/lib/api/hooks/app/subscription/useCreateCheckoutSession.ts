import { useMutation } from "@tanstack/react-query";
import createCheckoutSession from "@/lib/api/client/app/subscription/createCheckoutSession";

export default function useCreateCheckoutSession() {
    return useMutation({
        mutationFn: (data: { plan_id: string; interval: string }) => createCheckoutSession(data),
    })
}
