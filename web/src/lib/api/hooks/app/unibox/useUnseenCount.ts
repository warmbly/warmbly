import { useQuery } from "@tanstack/react-query";
import getUnseenCount from "@/lib/api/client/app/unibox/getUnseenCount";

export default function useUnseenCount() {
    return useQuery({
        queryKey: ["unibox", "unseen-count"],
        queryFn: () => getUnseenCount(),
    })
}
