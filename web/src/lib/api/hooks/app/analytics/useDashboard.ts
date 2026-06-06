import { useQuery } from "@tanstack/react-query";
import getDashboard from "@/lib/api/client/app/analytics/getDashboard";

export default function useDashboard(period: string = "7d") {
    return useQuery({
        queryKey: ["analytics", "dashboard", period],
        queryFn: () => getDashboard(period),
    })
}
