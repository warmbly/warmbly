import type SubscriptionLimits from "@/lib/api/models/app/subscription/SubscriptionLimits";
import Request from "../../Request";

export default async function getSubscriptionLimits(): Promise<SubscriptionLimits> {
    return await Request<SubscriptionLimits>({
        method: "GET",
        url: `/subscription/limits`,
        authorization: true,
    })
}
