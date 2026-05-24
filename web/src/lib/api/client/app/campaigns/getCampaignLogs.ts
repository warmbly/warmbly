import Request from "../../Request";

export default async function getCampaignLogs(id: string): Promise<{ logs: { timestamp: Date; message: string; level: string }[] }> {
    return await Request<{ logs: { timestamp: Date; message: string; level: string }[] }>({
        method: "GET",
        url: `/campaigns/${id}/logs`,
        authorization: true,
    })
}
