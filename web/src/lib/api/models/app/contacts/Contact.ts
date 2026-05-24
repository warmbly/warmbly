import type MiniCampaign from "../campaigns/MiniCampaign";

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

    updated_at: Date;
    created_at: Date;
}
