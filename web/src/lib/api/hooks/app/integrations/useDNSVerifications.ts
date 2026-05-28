import { useQuery } from "@tanstack/react-query";
import listDNSVerifications from "@/lib/api/client/app/integrations/listDNSVerifications";

export default function useDNSVerifications() {
    return useQuery({
        queryKey: ["integrations", "dns", "verifications"],
        queryFn: listDNSVerifications,
        staleTime: 10_000,
    });
}
