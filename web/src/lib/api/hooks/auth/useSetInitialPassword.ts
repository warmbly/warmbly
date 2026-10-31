import { useMutation } from "@tanstack/react-query";
import setInitialPassword from "../../client/auth/setInitialPassword";

export default function useSetInitialPassword() {
    return useMutation({
        mutationFn: ({ pending_token, password, device_token }: { pending_token: string; password: string; device_token?: string }) =>
            setInitialPassword(pending_token, password, device_token),
    });
}
