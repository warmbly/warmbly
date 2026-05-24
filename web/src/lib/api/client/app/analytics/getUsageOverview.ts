import type UsageOverview from "@/lib/api/models/app/analytics/UsageOverview";
import Request from "../../Request";

export default async function getUsageOverview(): Promise<UsageOverview> {
    return await Request<UsageOverview>({
        method: "GET",
        url: `/analytics/usage`,
        authorization: true,
    })
}
