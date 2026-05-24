import type ResetPasswordConfirm from "../../models/auth/ResetPasswordConfirm";
import Request from "../Request";

export default async function resetPasswordConfirm(data: ResetPasswordConfirm): Promise<void> {
    return await Request<void>({
        method: "POST",
        url: "/auth/reset-password/confirm",
        data,
    })
}
