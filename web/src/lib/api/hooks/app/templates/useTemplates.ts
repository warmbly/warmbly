import { useQuery } from "@tanstack/react-query";
import listTemplates from "@/lib/api/client/app/templates/listTemplates";

export default function useTemplates() {
    return useQuery({
        queryKey: ["templates", "list"],
        queryFn: () => listTemplates(),
    })
}
