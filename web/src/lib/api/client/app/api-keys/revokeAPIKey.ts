import Request from "../../Request";

export default async function revokeAPIKey(id: string, reason?: string): Promise<void> {
    const qs = reason ? `?reason=${encodeURIComponent(reason)}` : "";
    return await Request<void>({
        method: "DELETE",
        url: `/api-keys/${id}${qs}`,
        authorization: true,
    });
}
