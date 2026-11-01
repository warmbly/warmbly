import { useQuery } from "@tanstack/react-query";
import getReauthMethods from "@/lib/api/client/auth/reauth/methods";

export default function useReauthMethods() {
    return useQuery({
        queryKey: ["auth", "reauth-methods"],
        queryFn: ({ signal }) => getReauthMethods(signal),
    });
}
