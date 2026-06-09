// An A/B test variant for a campaign. When sequence_id is set the variant is
// scoped to that step; null = campaign-level (applies to every step).
export default interface ABVariant {
    id: string;
    campaign_id: string;
    sequence_id?: string | null;
    name: string;
    weight: number;
    subject: string;
    body_html: string;
    body_plain: string;
    is_control: boolean;
    is_active: boolean;
    created_at: Date;
    updated_at: Date;
}

export interface CreateABVariantInput {
    name: string;
    sequence_id?: string | null;
    weight?: number;
    subject?: string;
    body_html?: string;
    body_plain?: string;
    is_control?: boolean;
    is_active?: boolean;
}

export interface UpdateABVariantInput {
    name?: string;
    weight?: number;
    subject?: string;
    body_html?: string;
    body_plain?: string;
    is_control?: boolean;
    is_active?: boolean;
}

// Per-variant performance (from GET /campaigns/:id/ab-analysis).
export interface ABVariantStats {
    variant_id: string;
    variant_name: string;
    total_sent: number;
    opened: number;
    clicked: number;
    replied: number;
    bounced: number;
    open_rate: number;
    click_rate: number;
    reply_rate: number;
    bounce_rate: number;
}

// The campaign's A/B winner analysis.
export interface ABWinnerAnalysis {
    campaign_id: string;
    variants: ABVariantStats[];
    winner_id?: string | null;
    winner_name?: string;
    winning_rule: string;
    confidence: string;
}
