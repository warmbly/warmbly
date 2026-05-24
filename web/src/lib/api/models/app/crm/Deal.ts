export type DealStatus = "open" | "won" | "lost";

export default interface Deal {
    id: string;
    organization_id: string;
    pipeline_id: string;
    stage_id: string;
    contact_id?: string;
    name: string;
    value?: number;
    currency: string;
    status: DealStatus;
    expected_close_date?: string;
    won_at?: string;
    lost_at?: string;
    lost_reason?: string;
    assigned_to?: string;
    created_at: string;
    updated_at: string;

    // Optional joined fields the API may return
    contact?: {
        id: string;
        first_name: string;
        last_name: string;
        email: string;
        company: string;
    };
    stage?: {
        id: string;
        name: string;
        color: string;
        position: number;
    };
}

export interface DealsResult {
    data: Deal[];
    pagination: {
        has_more: boolean;
        next_cursor?: string | null;
    };
}
