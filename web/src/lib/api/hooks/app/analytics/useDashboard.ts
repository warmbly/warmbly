import { useQuery } from "@tanstack/react-query";
import getDashboard from "@/lib/api/client/app/analytics/getDashboard";

export default function useDashboard() {
    return useQuery({
        queryKey: ["analytics", "dashboard"],
        queryFn: () => getDashboard(),
    })
}
