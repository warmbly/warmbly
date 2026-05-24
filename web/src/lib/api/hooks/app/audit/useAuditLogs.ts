import { useQuery } from "@tanstack/react-query";
import getAuditLogs, { type GetAuditLogsParams } from "@/lib/api/client/app/audit/getAuditLogs";

export default function useAuditLogs(params: GetAuditLogsParams = {}) {
    return useQuery({
        queryKey: ["audit", "list", params],
        queryFn: () => getAuditLogs(params),
        staleTime: 30_000,
    });
}
