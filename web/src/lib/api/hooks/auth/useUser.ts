import { useQuery } from "@tanstack/react-query";
import getUser from "../../client/auth/getUser";

// Identity rarely changes. We override the global 30s default with a
// long staleTime so navigating between pages never refetches — only
// explicit invalidations (avatar upload, onboarding completion) move
// it.
export default function useUser() {
    return useQuery({
        queryKey: ["auth", "me"],
        queryFn: () => getUser(),
        staleTime: 5 * 60_000,
        gcTime: 30 * 60_000,
        refetchOnMount: false,
    });
}
