import type MiniCampaign from "../campaigns/MiniCampaign";
import type MiniCategory from "./MiniCategory";

// LeadStatus mirrors models.ContactCampaignProgress.Status — a contact's
// processing state inside a single campaign. "completed" = every step sent, no
// reply (done); "active" = some but not all steps sent (still processing).
export type LeadStatus =
    | "pending"
    | "active"
    | "completed"
    | "replied"
    | "bounced"
    | "unsubscribed";

// ContactCampaignProgress is set only on contacts returned by a single-campaign
// (Leads view) search; it summarises how far the lead is through the campaign.
export interface ContactCampaignProgress {
    status: LeadStatus;
    sent: number;
    opened: number;
    clicked: number;
    replied: number;
    bounced: number;
    last_activity_at?: string | null;
    // Label of the step the lead is on now (latest step sent). Empty when the
    // lead hasn't been contacted yet.
    current_step?: string;
}

export default interface Contact {
    id: string;

    first_name: string;
    last_name: string;
    email: string;
    company: string;
    phone: string;

    custom_fields: Record<string, string>;

    subscribed: boolean;
    campaigns: MiniCampaign[];
    categories: MiniCategory[];

    // Present only in the campaign Leads view (single-campaign search). Drives
    // the per-lead processing-state column.
    campaign_lead?: ContactCampaignProgress | null;

    updated_at: Date;
    created_at: Date;
}
