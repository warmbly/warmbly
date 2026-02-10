import { useQuery } from "@tanstack/react-query";
import getMyInvitations from "@/lib/api/client/app/organizations/getMyInvitations";

export default function useMyInvitations() {
    return useQuery({
        queryKey: ["invitations", "mine"],
        queryFn: () => getMyInvitations(),
    })
}
