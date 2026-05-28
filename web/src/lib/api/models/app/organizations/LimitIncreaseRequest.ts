export type LimitRequestStatus =
    | "pending"
    | "approved"
    | "rejected"
    | "cancelled";

export type LimitField =
    | "max_campaigns"
    | "max_active_campaigns"
    | "max_team_members"
    | "max_email_accounts"
    | "max_contacts"
    | "daily_campaign_limit";

export default interface LimitIncreaseRequest {
    id: string;
    organization_id: string;
    field: LimitField;
    current_effective: number;
    requested: number;
    reason: string;
    status: LimitRequestStatus;
    submitted_by: string;
    submitted_at: string;
    reviewed_by?: string | null;
    reviewed_at?: string | null;
    review_notes: string;
}

export interface CreateLimitIncreaseRequest {
    field: LimitField;
    requested: number;
    reason: string;
}
