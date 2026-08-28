import type { PreflightReport } from "@/lib/api/models/app/campaigns/Preflight";
import Request from "../../Request";

// Read-only despite being a POST: it computes a report and stores nothing the
// caller can collide with, so retries are naturally safe.
export default async function runPreflight(campaignId: string): Promise<PreflightReport> {
    return await Request<PreflightReport>({
        method: "POST",
        url: `/campaigns/${campaignId}/preflight`,
        authorization: true,
    });
}
