import { useQuery } from "@tanstack/react-query";
import listPasskeys from "@/lib/api/client/auth/passkey/listCredentials";

export default function usePasskeys() {
    return useQuery({
        queryKey: ["passkeys"],
        queryFn: listPasskeys,
    });
}
