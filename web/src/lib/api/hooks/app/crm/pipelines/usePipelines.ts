import { useQuery } from "@tanstack/react-query";
import listPipelines from "@/lib/api/client/app/crm/pipelines/listPipelines";

export default function usePipelines() {
    return useQuery({
        queryKey: ["crm", "pipelines", "list"],
        queryFn: () => listPipelines(),
    })
}
