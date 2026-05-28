import Request from "../../Request";

export default async function cancelLimitRequest(id: string): Promise<void> {
    await Request<void>({
        method: "DELETE",
        url: `/limit-requests/${id}`,
        authorization: true,
    });
}
