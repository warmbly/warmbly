import Request from "../../Request";

export default async function cancelSubscription(): Promise<void> {
    return await Request<void>({
        method: "POST",
        url: `/subscription/cancel`,
        authorization: true,
    })
}
