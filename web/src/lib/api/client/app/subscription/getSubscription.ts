import type Subscription from "@/lib/api/models/app/subscription/Subscription";
import Request from "../../Request";

export default async function getSubscription(): Promise<Subscription> {
    return await Request<Subscription>({
        method: "GET",
        url: `/subscription`,
        authorization: true,
    })
}
