import type { ContactSocial } from "@/lib/api/models/app/social/Social";
import type MiniCampaign from "../campaigns/MiniCampaign";
import type MiniCategory from "./MiniCategory";
import type { MailHost } from "../emails/MailboxImport";

// LeadStatus mirrors models.ContactCampaignProgress.Status, a contact's
// processing state inside a single campaign. "completed" = every step sent, no
// reply (done); "active" = some but not all steps sent (still processing);
// "failed" = the mailbox could not send a step after every retry;
// "paused" = the lead's flow is held (an out-of-office auto-reply, or a member
// pausing it) and resumes where it stopped;
// "undeliverable" = address verification refused it, so the campaign skips it.
export type LeadStatus =
    | "pending"
    | "active"
    | "completed"
    | "replied"
    | "bounced"
    | "failed"
    | "unsubscribed"
    | "paused"
    | "undeliverable";

// One contact's flow parked inside one campaign. source is "out_of_office"
// when an auto-reply parked it, "manual" when a member did, "crm" when the
// connected CRM says the contact moved on (a HubSpot deal or lifecycle stage, a
// Salesforce rule), and "cc" while the contact is copied on another lead's
// emails (reason is that lead's address), and "review" while a review campaign
// waits for someone to approve the lead's first email;
// `until` absent means the hold has no end and only a resume lifts it.
export interface LeadHold {
    since: Date;
    until?: Date | null;
    reason?: string;
    source: "manual" | "out_of_office" | "inbox_tagging" | "cc" | "crm" | "review" | string;
}

// Why a copied contact is or is not on the next email to the lead.
export type LeadCCStatus = "active" | "unsubscribed" | "bounced" | "undeliverable";

// A contact copied on every email one campaign sends one lead.
export interface LeadCC {
    contact_id: string;
    email: string;
    first_name: string;
    last_name: string;
    company?: string;
    status: LeadCCStatus;
    bounced_at?: Date | null;
}

// The most contacts one lead can copy; mirrors config.CampaignLeadMaxCC.
export const LEAD_CC_MAX = 2;

export function leadCCName(cc: Pick<LeadCC, "first_name" | "last_name" | "email">): string {
    return `${cc.first_name ?? ""} ${cc.last_name ?? ""}`.trim() || cc.email;
}

// holdSummary is the one sentence a held lead gets, wherever it is shown: why
// the flow is parked and when it lifts. One function so the Leads row and the
// contact drawer cannot word the same hold two different ways.
export function holdSummary(hold: LeadHold): string {
    if (hold.source === "review") return "Waiting for review · their first email goes out once someone approves it";
    if (hold.source === "cc") {
        return hold.reason
            ? `Copied on the emails to ${hold.reason} · none of their own are sent`
            : "Copied on another lead's emails · none of their own are sent";
    }
    // A CRM hold's reason already names the CRM ("Salesforce: Lead Status is Qualified").
    const what =
        hold.source === "out_of_office"
            ? "Out of office"
            : hold.source === "crm"
              ? hold.reason
                  ? "Held"
                  : "Held by your CRM"
              : "Paused";
    const why = hold.reason ? ` · ${hold.reason}` : "";
    if (!hold.until) return `${what}${why} · until someone resumes it`;
    const until = new Date(hold.until).toLocaleString(undefined, {
        month: "short",
        day: "numeric",
        hour: "numeric",
        minute: "2-digit",
    });
    return `${what}${why} · until ${until}`;
}

// LeadEngagement mirrors models.SearchContacts.Engagement. "opened" is a human
// open (machine opens never count); the not_* forms match leads that were sent
// at least one step, so a queued lead is neither opened nor not opened.
export type LeadEngagement =
    | "opened"
    | "not_opened"
    | "clicked"
    | "not_clicked"
    | "replied"
    | "not_replied"
    | "bounced";

// ContactCampaignProgress is set only on contacts returned by a single-campaign
// (Leads view) search; it summarises how far the lead is through the campaign.
export interface ContactCampaignProgress {
    status: LeadStatus;
    sent: number;
    // Human opens; automated fetches (Apple MPP prefetch, UA-less clients)
    // are counted apart in machine_opened.
    opened: number;
    machine_opened: number;
    clicked: number;
    replied: number;
    bounced: number;
    last_activity_at?: Date | null;
    // Label of the step the lead is on now (latest step sent). Empty when the
    // lead hasn't been contacted yet.
    current_step?: string;
    // The mailbox this lead's whole sequence sends from, fixed when its first
    // email went out. Empty until then.
    sender?: string;
    // The worker's reason for the last failed send; set only when status is
    // "failed".
    failure_reason?: string;
    // The live hold, when the lead's flow is parked. Present on any status: a
    // held lead that also replied still reads "replied".
    hold?: LeadHold | null;
    // Contacts copied on every email to this lead in this campaign.
    cc?: LeadCC[];
}

// VerificationStatus mirrors emailverify.Status: the pre-send verdict on the
// address. "invalid" is never sent to; "risky" (catch-all, role) only when the
// campaign allows it; "unknown" and "valid" always send.
export type VerificationStatus = "valid" | "risky" | "invalid" | "unknown";

// VerificationSource says who produced the verdict: the in-house probe, a
// connected provider, a status column imported with the list, or a member.
export type VerificationSource = "" | "probe" | "provider" | "imported" | "manual";

export default interface Contact {
    id: string;

    first_name: string;
    last_name: string;
    email: string;
    company: string;
    phone: string;
    // The contact's handles by channel, each with the consent basis recorded for outreach there.
    social?: ContactSocial | null;

    custom_fields: Record<string, string>;

    subscribed: boolean;
    campaigns: MiniCampaign[];
    categories: MiniCategory[];

    verification_status?: VerificationStatus;
    verification_reason?: string;
    verification_sub_status?: string;
    verification_source?: VerificationSource;
    verification_provider?: string;
    verification_checked_at?: Date | null;
    // How sure the platform is of the status, 0 to 100, scored from the last
    // check plus what real mail to the address showed.
    verification_confidence?: number;
    // Set while a re-check a member asked for waits to run; the verdict
    // above stands until it lands.
    verification_requested_at?: Date | null;
    is_catch_all?: boolean;

    // Who hosts the contact's inbox, read from its domain's MX; "" until
    // detected. esp_provider is its family, the one ESP matching uses.
    mail_host?: MailHost;
    esp_provider?: "" | "gmail" | "outlook" | "other";

    // Present only in the campaign Leads view (single-campaign search). Drives
    // the per-lead processing-state column.
    campaign_lead?: ContactCampaignProgress | null;

    updated_at: Date;
    created_at: Date;
}
