import { useMutation } from "@tanstack/react-query";
import enterpriseInquiry from "@/lib/api/client/app/subscription/enterpriseInquiry";

export default function useEnterpriseInquiry() {
    return useMutation({
        mutationFn: (data: { name: string; email: string; company: string; message?: string }) => enterpriseInquiry(data),
    })
}
