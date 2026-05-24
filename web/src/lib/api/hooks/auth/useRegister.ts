import { useMutation } from "@tanstack/react-query";
import type Register from "../../models/auth/Register";
import register from "../../client/auth/register";

export default function useRegister() {
    return useMutation({
        mutationFn: (data: Register) => register(data)
    })
}
