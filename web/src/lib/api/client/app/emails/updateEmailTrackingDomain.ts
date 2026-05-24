import type TrackingDomain from "@/lib/api/models/app/emails/TrackingDomain";
import Request from "../../Request";

export default async function updateEmailTrackingDomain(id: string, domain: string): Promise<TrackingDomain> {
    return await Request<TrackingDomain>({
        method: "PATCH",
        url: `/emails/${id}/track?domain=${domain}`,
        authorization: true,
    })
}
