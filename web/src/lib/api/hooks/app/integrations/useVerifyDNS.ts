import { useMutation, useQueryClient } from "@tanstack/react-query";
import verifyDNS, { type DNSVerifyInput } from "@/lib/api/client/app/integrations/verifyDNS";

export default function useVerifyDNS() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (input: DNSVerifyInput) => verifyDNS(input),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ["integrations", "dns", "verifications"] });
        },
    });
}
