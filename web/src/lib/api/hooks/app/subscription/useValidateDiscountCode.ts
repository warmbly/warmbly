import { useMutation } from "@tanstack/react-query";
import validateDiscountCode, {
    type ValidateDiscountInput,
} from "@/lib/api/client/app/subscription/validateDiscountCode";

export default function useValidateDiscountCode() {
    return useMutation({
        mutationFn: (data: ValidateDiscountInput) => validateDiscountCode(data),
    });
}
