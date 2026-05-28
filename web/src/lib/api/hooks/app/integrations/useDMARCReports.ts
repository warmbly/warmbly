import { useQuery } from "@tanstack/react-query";
import listDMARCReports from "@/lib/api/client/app/integrations/listDMARCReports";

export default function useDMARCReports(domain?: string) {
    return useQuery({
        queryKey: ["integrations", "dmarc", domain ?? ""],
        queryFn: () => listDMARCReports(domain),
        staleTime: 30_000,
    });
}
