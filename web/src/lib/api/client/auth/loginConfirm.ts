import type Token from "../../models/auth/Token";
import Request from "../Request";
import type LoginConfirm from "../../models/auth/LoginConfirm";

export default async function loginConfirm(data: LoginConfirm): Promise<Token> {
    return await Request<Token>({
        method: "POST",
        url: "/auth/login/confirm",
        data,
    });
}
