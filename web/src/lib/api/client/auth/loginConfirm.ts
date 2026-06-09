import type { LoginResult } from "../../models/auth/LoginResult";
import Request from "../Request";
import type LoginConfirm from "../../models/auth/LoginConfirm";

export default async function loginConfirm(data: LoginConfirm): Promise<LoginResult> {
    return await Request<LoginResult>({
        method: "POST",
        url: "/auth/login/confirm",
        data,
    });
}
