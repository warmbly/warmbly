import { useMutation } from "@tanstack/react-query";
import type RegisterConfirm from "../../models/auth/RegisterConfirm";
import registerConfirm from "../../client/auth/registerConfirm";

export default function useRegisterConfirm() {
    return useMutation({
        mutationFn: (data: RegisterConfirm) => registerConfirm(data)
    })
}
