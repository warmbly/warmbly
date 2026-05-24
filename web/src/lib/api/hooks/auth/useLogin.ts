import { useMutation } from "@tanstack/react-query";
import type Login from "../../models/auth/Login";
import login from "../../client/auth/login";

export default function useLogin() {
    return useMutation({
        mutationFn: (data: Login) => login(data)
    })
}
