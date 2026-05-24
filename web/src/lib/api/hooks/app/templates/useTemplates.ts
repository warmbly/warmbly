import { useQuery } from "@tanstack/react-query";
import listTemplates from "@/lib/api/client/app/templates/listTemplates";

export default function useTemplates(search?: string) {
    return useQuery({
        queryKey: ["templates", "list", search ?? ""],
        queryFn: () => listTemplates(search),
    });
}
