import type Token from "../../models/auth/Token";
import type RefreshToken from "../../models/auth/RefreshToken";
import Request from "../Request";

export default async function refreshToken(refresh_token: string): Promise<Token> {
    const data: RefreshToken = {
        refresh_token,
    }

    const res = await Request<Token>({
        method: "POST",
        url: "/auth/refresh",
        data,
    })

    return res
}
