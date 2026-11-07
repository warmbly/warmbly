// Where a campaign step's copy landed when its senders' warmup carried it.

export interface WarmupCopyPlacement {
    sent: number;
    // Sends whose recipient filed a receipt; landing rates are read over this.
    reported: number;
    inbox: number;
    tabs: number;
    spam: number;
    rescued: number;
}

// google is Google Workspace and microsoft is Microsoft 365.
export type WarmupProviderGroup = "google" | "microsoft";

export interface WarmupCopyGroupPlacement extends WarmupCopyPlacement {
    group: WarmupProviderGroup | (string & {});
}

export interface WarmupStepPlacement {
    sequence_id: string;
    // Null is the step's own copy (the Original); otherwise one of its A/B variants.
    variant_id: string | null;
    // Business inboxes on Google Workspace and Microsoft 365, the only ones campaign copy is sent to.
    placement: WarmupCopyPlacement;
    by_group: WarmupCopyGroupPlacement[];
}

export interface CampaignWarmupPlacement {
    days: number;
    data: WarmupStepPlacement[];
}
