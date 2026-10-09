import { useQuery } from "@tanstack/react-query";
import { useMe } from "@/hooks/useMe";
import { AdminPerm } from "@/lib/auth/permissions";
import { canMonitor } from "@/lib/monitoring";
import { getInstanceMonitoring } from "@/lib/api/client/admin/monitoring";

export const INSTANCE_MONITORING_KEY = ["admin", "instance", "monitoring"] as const;

export function useInstanceMonitoring() {
    const { data: me } = useMe();
    return useQuery({
        queryKey: [...INSTANCE_MONITORING_KEY, me?.id, me?.admin_permissions],
        queryFn: getInstanceMonitoring,
        enabled: canMonitor(me?.admin_permissions, AdminPerm.ViewAnalytics),
        staleTime: 60_000,
        refetchInterval: 60_000,
        refetchIntervalInBackground: false,
        refetchOnWindowFocus: false,
        retry: false,
    });
}
