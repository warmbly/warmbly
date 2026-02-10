import { useQuery } from "@tanstack/react-query";
import getEmail from "@/lib/api/client/app/unibox/getEmail";

export default function useUniboxEmail(id: string) {
    return useQuery({
        queryKey: ["unibox", "email", id],
        queryFn: () => getEmail(id),
        enabled: !!id,
    })
}
