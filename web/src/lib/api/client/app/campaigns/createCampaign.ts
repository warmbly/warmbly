import type Campaign from "@/lib/api/models/app/campaigns/Campaign";
import Request from "../../Request";

// Wire shape for POST /campaigns. Everything other than `name` is optional;
// the backend fills defaults for anything we don't send. The wizard sends
// the full payload in one shot so we never leave a half-configured campaign.
export interface CreateCampaignInput {
    name: string;
    description?: string;

    // Sending rules / tracking
    stop_on_reply?: boolean;
    open_tracking?: boolean;
    link_tracking?: boolean;
    text_only?: boolean;
    daily_limit?: number;
    unsubscribe_header?: boolean;
    risky_emails?: boolean;

    cc?: string[];
    bcc?: string[];

    // Schedule
    start_date?: string | null;
    end_date?: string | null;
    timezone?: string;
    days?: number;
    start_time?: string;
    end_time?: string;

    // Sender pool (existing user-owned tag/folder UUIDs)
    email_tag_ids?: string[];
    folder_ids?: string[];

    // Net-new send controls (all optional; backend defaults reproduce today's behavior)
    sender_strategy?: 'tags' | 'explicit';
    rotation_mode?: 'weighted' | 'round_robin' | 'least_recently_used';
    ramp_enabled?: boolean;
    ramp_start?: number;
    ramp_increment?: number;
    ramp_ceiling?: number;
    esp_match_mode?: 'off' | 'prefer' | 'strict';
    max_new_leads_per_day?: number;
    prioritize_new_leads?: boolean;
    tracking_domain?: string;

    // Initial sequences (ordered)
    steps?: Array<{
        name: string;
        subject: string;
        body_plain: string;
        body_html: string;
        body_sync?: boolean;
        body_code?: boolean;
        wait_after?: number;
    }>;

    // A/B variants for the first step
    variants?: Array<{
        name: string;
        weight?: number;
        subject?: string;
        body_html?: string;
        body_plain?: string;
        is_control?: boolean;
        is_active?: boolean;
    }>;
}

export default async function createCampaign(input: CreateCampaignInput): Promise<Campaign> {
    return await Request<Campaign>({
        method: "POST",
        url: `/campaigns`,
        data: input,
        authorization: true,
    });
}
