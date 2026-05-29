import { useMutation } from "@tanstack/react-query";
import createCheckoutSession, {
    type CreateCheckoutInput,
} from "@/lib/api/client/app/subscription/createCheckoutSession";

export default function useCreateCheckoutSession() {
    return useMutation({
        mutationFn: (data: CreateCheckoutInput) => createCheckoutSession(data),
    });
}
