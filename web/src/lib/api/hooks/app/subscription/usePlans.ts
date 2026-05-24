import { useQuery } from "@tanstack/react-query";
import listPlans from "@/lib/api/client/app/subscription/listPlans";

export default function usePlans() {
    return useQuery({
        queryKey: ["plans"],
        queryFn: () => listPlans(),
    })
}
