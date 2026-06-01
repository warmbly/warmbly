import Request from "../../Request";
import type Token from "@/lib/api/models/auth/Token";
import type { AuthenticationResponseJSON } from "@simplewebauthn/browser";

export default async function passkeyLoginFinish(data: {
    session: string;
    credential: AuthenticationResponseJSON;
}): Promise<Token> {
    return await Request<Token>({
        method: "POST",
        url: "/auth/passkey/login/finish",
        data,
    });
}
