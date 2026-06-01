import Request from "../../Request";
import type Passkey from "@/lib/api/models/auth/Passkey";
import type { RegistrationResponseJSON } from "@simplewebauthn/browser";

export default async function passkeyRegisterFinish(data: {
    name?: string;
    credential: RegistrationResponseJSON;
}): Promise<Passkey> {
    return await Request<Passkey>({
        method: "POST",
        url: "/auth/passkey/register/finish",
        authorization: true,
        data,
    });
}
