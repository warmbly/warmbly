import { useQuery } from "@tanstack/react-query";
import getTemplate from "@/lib/api/client/app/templates/getTemplate";

export default function useTemplate(id: string) {
    return useQuery({
        queryKey: ["templates", id],
        queryFn: () => getTemplate(id),
        enabled: !!id,
    })
}
