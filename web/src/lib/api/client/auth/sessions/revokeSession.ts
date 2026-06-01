import Request from "../../Request";

export default async function revokeSession(id: string): Promise<void> {
    await Request<void>({
        method: "DELETE",
        url: `/auth/sessions/${id}`,
        authorization: true,
    });
}
