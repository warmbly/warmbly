import Request from "../../Request";
import type Passkey from "@/lib/api/models/auth/Passkey";

export default async function renamePasskey(id: string, name: string): Promise<Passkey> {
    return await Request<Passkey>({
        method: "PATCH",
        url: `/auth/passkey/credentials/${id}`,
        authorization: true,
        data: { name },
    });
}
