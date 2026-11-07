// An A/B test variant for a campaign. When step_id is set the variant is
// scoped to that step; null = campaign-level (applies to every step).
export default interface ABVariant {
    id: string;
    campaign_id: string;
    step_id?: string | null;
    name: string;
    weight: number;
    subject: string;
    body_html: string;
    body_plain: string;
    is_control: boolean;
    is_active: boolean;
    // Set when auto-promotion made this arm its step's winner.
    promoted_at?: string | null;
    created_at: Date;
    updated_at: Date;
}

export interface CreateABVariantInput {
    name: string;
    step_id?: string | null;
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

// One arm of a step split, counted in contacts. Rates are over delivered
// contacts (sent minus bounced). variant_id is null for the Original.
export interface ABStepArmStats {
    variant_id: string | null;
    name: string;
    is_control: boolean;
    is_active: boolean;
    sent: number;
    bounced: number;
    delivered: number;
    opened: number;
    clicked: number;
    replied: number;
    open_rate: number;
    click_rate: number;
    reply_rate: number;
    bounce_rate: number;
}

export interface ABStepWinner {
    variant_id: string | null;
    is_control: boolean;
    name: string;
}

export type ABConfidence = "none" | "low" | "medium" | "high";

// One step's split: every arm, and the winner once its lead is significant.
export interface ABStepAnalysis {
    step_id: string;
    arms: ABStepArmStats[];
    winner?: ABStepWinner | null;
    confidence: ABConfidence;
    p_value?: number | null;
    promoted_at?: string | null;
}

// The campaign's A/B winner analysis.
export interface ABWinnerAnalysis {
    campaign_id: string;
    variants: ABVariantStats[];
    winner_id?: string | null;
    winner_name?: string;
    winning_rule: string;
    confidence: string;
    steps?: ABStepAnalysis[];
}
