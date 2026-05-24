import type FeatureStatus from "@/lib/api/models/app/subscription/FeatureStatus";
import Request from "../../Request";

export default async function getFeatureStatus(): Promise<FeatureStatus> {
    return await Request<FeatureStatus>({
        method: "GET",
        url: `/subscription/features`,
        authorization: true,
    })
}
