import type { LeadCC, LeadHold, LeadStatus } from "./Contact";
import type { FollowUpSender } from "../campaigns/Campaign";

// One campaign a contact belongs to, as the Activity tab's campaign panel
// shows it: the flow with this contact's progress, the derived lead status,
// and a scheduler-backed next action.
export interface ContactCampaignStep {
    id: string;
    label: string;
    kind: string;
    position: number;
    subject?: string;
    // The mailbox that sent this step; absent until it is sent.
    sender_email?: string;

    sent_at?: Date | null;
    opened_at?: Date | null;
    clicked_at?: Date | null;
    replied_at?: Date | null;
    bounced_at?: Date | null;
    failed_at?: Date | null;
    attempts?: number;
    in_flight?: boolean;
}

// due: the step is due; scheduled_at is when the campaign next works its queue.
// waiting: a hard constraint holds it back; not_before is the earliest.
// paused: the lead's own flow is held, or the campaign is not active.
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
    scheduled_at?: Date | null;
    not_before?: Date | null;
    constraint?: string;
}

export default interface ContactCampaignState {
    campaign_id: string;
    campaign_name: string;
    campaign_status: string;
    lead_status: LeadStatus;
    failure_reason?: string;

    // The mailbox that sent this lead's latest step. Rotation picks it for the
    // first email and every follow-up keeps it unless follow_up_sender rotates
    // them. Absent until the first email goes out.
    sender_id?: string | null;
    sender_email?: string;
    follow_up_sender?: FollowUpSender;

    steps: ContactCampaignStep[];
    completed_steps: number;
    total_steps: number;

    current_step?: ContactCampaignStep | null;
    last_action?: string;
    last_action_at?: Date | null;

    // The live per-lead hold: an out-of-office auto-reply parked the contact,
    // or a member paused them. Absent when the lead is not held.
    hold?: LeadHold | null;

    // Contacts copied on every email to this lead in this campaign.
    cc?: LeadCC[];

    next?: ContactNextAction | null;
    ended_reason?: string;
}

export interface ContactCampaignStatesResult {
    data: ContactCampaignState[];
}
