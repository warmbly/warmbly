import { useMutation } from "@tanstack/react-query";
import type ResetPassword from "../../models/auth/ResetPassword";
import resetPassword from "../../client/auth/resetPassword";

export default function useResetPassword() {
    return useMutation({
        mutationFn: (data: ResetPassword) => resetPassword(data)
    })
}
