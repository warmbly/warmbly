import Request from "../../Request";
import type PasskeyRegisterBegin from "@/lib/api/models/auth/PasskeyRegisterBegin";

export default async function passkeyRegisterBegin(): Promise<PasskeyRegisterBegin> {
    return await Request<PasskeyRegisterBegin>({
        method: "POST",
        url: "/auth/passkey/register/begin",
        authorization: true,
    });
}
