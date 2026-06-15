import Request from "../../Request";

export default async function deleteWebhook(id: string): Promise<void> {
    await Request<void>({
        method: "DELETE",
        url: `/webhooks/${id}`,
        authorization: true,
    });
}
