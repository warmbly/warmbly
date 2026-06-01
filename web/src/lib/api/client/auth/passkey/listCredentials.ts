import Request from "../../Request";
import type Passkey from "@/lib/api/models/auth/Passkey";

export default async function listPasskeys(): Promise<Passkey[]> {
    return await Request<Passkey[]>({
        method: "GET",
        url: "/auth/passkey/credentials",
        authorization: true,
    });
}
