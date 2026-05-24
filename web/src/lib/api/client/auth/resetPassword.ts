import type ResetPassword from "../../models/auth/ResetPassword";
import Request from "../Request";

export default async function resetPassword(data: ResetPassword): Promise<void> {
    return await Request<void>({
        method: "POST",
        url: "/auth/reset-password",
        data,
    })
}
