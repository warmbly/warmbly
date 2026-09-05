import type MailboxAllowance from "@/lib/api/models/app/emails/MailboxAllowance";

/** GET /organization/current/limits: what the server enforces beside live counts.
 *  A null or absent limit is unmetered. */
export default interface OrganizationLimits {
    limits: {
        max_campaigns?: number | null;
        max_active_campaigns?: number | null;
        max_team_members?: number | null;
        max_email_accounts?: number | null;
        max_contacts?: number | null;
        daily_campaign_limit?: number | null;
    };
    counts: {
        total_campaigns: number;
        active_campaigns: number;
        total_contacts: number;
        total_members: number;
        email_accounts: number;
        emails_sent_today: number;
    };
    mailboxes: MailboxAllowance;
    storage: {
        used_bytes: number;
        limit_bytes: number;
        over_quota?: boolean;
    };
}
