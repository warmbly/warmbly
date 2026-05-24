import { useMutation } from "@tanstack/react-query";
import resetPasswordConfirm from "../../client/auth/resetPasswordConfirm";
import type ResetPasswordConfirm from "../../models/auth/ResetPasswordConfirm";

export default function useResetPasswordConfirm() {
    return useMutation({
        mutationFn: (data: ResetPasswordConfirm) => resetPasswordConfirm(data)
    })
}
