export type ContactTimelineEventType =
    | "email_sent"
    | "email_opened"
    | "email_clicked"
    | "email_replied"
    | "email_bounced"
    | "reply_received"
    | "deliverability"
    | "suppressed"
    | "note"
    | "meeting_booked"
    | "meeting_rescheduled"
    | "meeting_canceled";

export default interface ContactTimelineEvent {
    type: ContactTimelineEventType;
    at: string;

    email_account_id?: string | null;
    email_account_email?: string | null;
    email_account_name?: string | null;

    campaign_id?: string | null;
    campaign_name?: string | null;
    sequence_id?: string | null;
    sequence_name?: string | null;

    task_id?: string | null;
    subject?: string | null;

    reason?: string | null;
    source?: string | null;
    provider?: string | null;
    intent?: string | null;
    content?: string | null;

    // Meeting events (meeting_booked / rescheduled / canceled).
    scheduled_for?: string | null;
    join_url?: string | null;
    meeting_state?: string | null;

    user_id?: string | null;
}

export interface ContactTimelineResult {
    data: ContactTimelineEvent[];
    has_more: boolean;
}
