import { useQuery } from "@tanstack/react-query";
import getConnection from "@/lib/api/client/app/integrations/getConnection";

export default function useConnectionDetail(id: string | null) {
    return useQuery({
        queryKey: ["integrations", "connection", id],
        queryFn: () => getConnection(id as string),
        enabled: !!id,
    });
}
