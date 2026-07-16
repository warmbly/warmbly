import { useMutation } from "@tanstack/react-query";
import createCreditCheckout, {
    type CreateCreditCheckoutInput,
} from "@/lib/api/client/app/subscription/createCreditCheckout";

// Starts a one-time Stripe Checkout for a top-up pack. Fulfillment (the actual
// credit grant) happens in the Stripe webhook, so the caller just redirects to
// checkout_url and the balance refreshes via the realtime spine on return.
export default function useCreateCreditCheckout() {
    return useMutation({
        mutationFn: (data: CreateCreditCheckoutInput) => createCreditCheckout(data),
    });
}
