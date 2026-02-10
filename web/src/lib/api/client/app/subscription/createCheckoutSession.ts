import Request from "../../Request";

export default async function createCheckoutSession(data: { plan_id: string; interval: string }): Promise<{ url: string }> {
    return await Request<{ url: string }>({
        method: "POST",
        url: `/subscription/checkout`,
        data,
        authorization: true,
    })
}
