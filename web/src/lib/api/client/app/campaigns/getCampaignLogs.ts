import Request from "../../Request";

// Raw row shape returned by GET /campaigns/:id/logs (CampaignLogsResult).
interface CampaignLogRow {
    id: string;
    event_type: string;
    message: string;
    metadata?: Record<string, unknown> | null;
    created_at: string;
}

export interface CampaignLogItem {
    timestamp: Date;
    message: string;
    level: string;
    event_type: string;
}

// The API returns { data: [...], pagination } newest-first. Map it into the flat
// log items the activity panel renders, deriving the row severity from
// metadata.level ("error" / "warn" / default "info") so scheduler failures tint
// red in the dashboard.
export default async function getCampaignLogs(id: string): Promise<{ logs: CampaignLogItem[] }> {
    const res = await Request<{ data: CampaignLogRow[] }>({
        method: "GET",
        url: `/campaigns/${id}/logs`,
        authorization: true,
    });
    const rows = res?.data ?? [];
    return {
        logs: rows.map((r) => ({
            timestamp: new Date(r.created_at),
            message: r.message,
            level: typeof r.metadata?.level === "string" ? (r.metadata.level as string) : "info",
            event_type: r.event_type,
        })),
    };
}
