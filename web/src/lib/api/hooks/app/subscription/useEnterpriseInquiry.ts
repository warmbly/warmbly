import { useMutation } from "@tanstack/react-query";
import enterpriseInquiry, {
    type EnterpriseInquiryInput,
} from "@/lib/api/client/app/subscription/enterpriseInquiry";

export default function useEnterpriseInquiry() {
    return useMutation({
        mutationFn: (data: EnterpriseInquiryInput) => enterpriseInquiry(data),
    });
}
