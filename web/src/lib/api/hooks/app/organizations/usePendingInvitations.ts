import { useQuery } from "@tanstack/react-query";
import getPendingInvitations from "@/lib/api/client/app/organizations/getPendingInvitations";

export default function usePendingInvitations() {
    return useQuery({
        queryKey: ["organizations", "invitations"],
        queryFn: () => getPendingInvitations(),
    })
}
