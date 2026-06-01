import Request from "../../Request";

export default async function deletePasskey(id: string): Promise<void> {
    await Request<void>({
        method: "DELETE",
        url: `/auth/passkey/credentials/${id}`,
        authorization: true,
    });
}
