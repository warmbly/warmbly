import type { CreditUsageOverview } from "@/lib/api/models/app/subscription/Credits";
import Request from "../../Request";

export default async function getCreditUsage(days = 30): Promise<CreditUsageOverview> {
    return await Request<CreditUsageOverview>({
        method: "GET",
        url: `/subscription/credits/usage?days=${days}`,
        authorization: true,
    });
}
