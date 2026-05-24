export default interface Inbox {
    id: string;
    email: string;
    name: string;
    signature_plain: string;
    signature_html: string;
    signature_sync: boolean;
    signature_code: boolean;
    tags: string[];
    provider: string;
    status: string;
    last_synced_at: Date;
    last_id?: number | null;
    campaign_limit: number;
    min_wait_time: number;
    reply_to: string;
    tracking_domain: string;
    warmup?: Date | null;
    warmup_base: number;
    warmup_max: number;
    warmup_increase: number;
    warmup_reply_rate: number;
    created_at: Date;
    updated_at: Date;
}
