import type WarmupPlacement from "@/lib/api/models/app/analytics/WarmupPlacement";
import Request from "../../Request";

// Omit emailId for the workspace report. from/to are YYYY-MM-DD (UTC days).
export default async function getWarmupPlacement(emailId: string | undefined, from: string, to: string): Promise<WarmupPlacement> {
    const params = new URLSearchParams({ from, to });
    if (emailId) params.set("email_id", emailId);
    return await Request<WarmupPlacement>({
        method: "GET",
        url: `/analytics/warmup/placement?${params.toString()}`,
        authorization: true,
    });
}
