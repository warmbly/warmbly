import type DeliverabilityDashboard from "@/lib/api/models/app/analytics/Deliverability";
import Request from "../../Request";

const DAYS: Record<string, number> = { "7d": 7, "30d": 30, "90d": 90 };

// Deliverability rollup. The endpoint wants RFC3339 from/to (not a period).
export default async function getDeliverability(range: "7d" | "30d" | "90d" = "7d"): Promise<DeliverabilityDashboard> {
    const to = new Date();
    const from = new Date(to.getTime() - (DAYS[range] ?? 7) * 86_400_000);
    return await Request<DeliverabilityDashboard>({
        method: "GET",
        url: `/analytics/deliverability?from=${from.toISOString()}&to=${to.toISOString()}`,
        authorization: true,
    });
}
