import type { LeadStatus } from "./Contact";

// One campaign a contact belongs to, as the Activity tab's campaign panel
// shows it: the flow with this contact's progress, the derived lead status,
// and a scheduler-backed next action.
export interface ContactCampaignStep {
    id: string;
    label: string;
    kind: string;
    position: number;
    subject?: string;

    sent_at?: string | null;
    opened_at?: string | null;
    clicked_at?: string | null;
    replied_at?: string | null;
    bounced_at?: string | null;
    failed_at?: string | null;
    attempts?: number;
    in_flight?: boolean;
}

// due: the step is due; scheduled_at is when the campaign next works its queue.
// waiting: a hard constraint holds it back; not_before is the earliest.
// paused: the campaign is not active.
// blocked: the campaign cannot send at all right now.
export type ContactNextActionState = "due" | "waiting" | "paused" | "blocked";

export interface ContactNextAction {
    // Absent while a condition window is open: the next step depends on how
    // the contact responds.
    step_id?: string | null;
    step_label: string;
    kind?: string;
    subject?: string;

    state: ContactNextActionState;
    // Only when due now, and then it is the campaign chain's own next wakeup,
    // not a slot reserved for this contact: leads queued ahead can still push
    // this step to a later pass. Absent while the chain is being re-seeded.
    scheduled_at?: string | null;
    not_before?: string | null;
    constraint?: string;
}

export default interface ContactCampaignState {
    campaign_id: string;
    campaign_name: string;
    campaign_status: string;
    lead_status: LeadStatus;
    failure_reason?: string;

    // The mailbox this lead's whole sequence sends from. Rotation picks it for
    // the first email and every follow-up keeps it, so the contact always hears
    // from one address. Absent until the first email goes out.
    sender_id?: string | null;
    sender_email?: string;

    steps: ContactCampaignStep[];
    completed_steps: number;
    total_steps: number;

    current_step?: ContactCampaignStep | null;
    last_action?: string;
    last_action_at?: string | null;

    next?: ContactNextAction | null;
    ended_reason?: string;
}

export interface ContactCampaignStatesResult {
    data: ContactCampaignState[];
}
