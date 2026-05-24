import { useMutation } from "@tanstack/react-query";
import type LoginConfirm from "../../models/auth/LoginConfirm";
import loginConfirm from "../../client/auth/loginConfirm";

export default function useLoginConfirm() {
    return useMutation({
        mutationFn: (data: LoginConfirm) => loginConfirm(data)
    })
}
