import Request from "../../Request";

export default async function testConnection(connectionId: string): Promise<{ sent: number }> {
    return await Request<{ sent: number }>({
        method: "POST",
        url: `/integrations/connections/${connectionId}/test`,
        authorization: true,
    });
}
