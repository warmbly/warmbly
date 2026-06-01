import { useQuery } from "@tanstack/react-query";
import listSessions from "@/lib/api/client/auth/sessions/listSessions";

export default function useSessions() {
    return useQuery({
        queryKey: ["sessions"],
        queryFn: listSessions,
    });
}
