import Request from "../../Request";

export default async function disconnectIntegration(id: string): Promise<void> {
    await Request<void>({
        method: "DELETE",
        url: `/integrations/connections/${id}`,
        authorization: true,
    });
}
