import { useQuery } from "@tanstack/react-query";
import getPipeline from "@/lib/api/client/app/crm/pipelines/getPipeline";

export default function usePipeline(id: string) {
    return useQuery({
        queryKey: ["crm", "pipelines", id],
        queryFn: () => getPipeline(id),
        enabled: !!id,
    })
}
