import Request from "../../Request";

export interface TemplatePreviewFrom {
    name: string;
    email: string;
}

export interface TemplatePreviewAttachment {
    id: string;
    filename: string;
    size: number;
    mime_type: string;
}

// One thing mail clients will do to this body that its author did not ask for.
// Advisory: findings never block a send.
export interface TemplateHtmlFinding {
    code: string;
    severity: "error" | "warning" | "info";
    message: string;
}

export interface TemplatePreview {
    subject: string;
    body_html: string;
    body_plain: string;
    errors?: string[]; // template parse errors (these block sending)
    unresolved?: string[]; // literal {{…}} tokens left after render
    // Present when account_id was given: the sender as recipients see it.
    from?: TemplatePreviewFrom;
    // Present when campaign_id was given and this step's send carries files.
    attachments?: TemplatePreviewAttachment[];
    // What Gmail, Outlook and Yahoo will do to this markup: what they strip,
    // what CSS they ignore, and whether the message is long enough to be
    // clipped. Sized on the body as it ships, after CSS inlining.
    html_findings?: TemplateHtmlFinding[];
    // The copy's AI blocks; written is true when this contact's copy is.
    ai_blocks?: { id: string; name: string; written: boolean }[];
    // The contact is a lead of the campaign, so its AI copy can be written ahead.
    lead?: boolean;
    // The lead has their own copy of this step, which they get instead.
    personalized?: boolean;
}

export interface TemplatePreviewInput {
    subject: string;
    body_html: string;
    body_plain: string;
    // A contact of the organization to render for; omitted = the built-in sample.
    contact_id?: string;
    // Applies the campaign's opt-out footer and plain-text rule and lists its attachments.
    campaign_id?: string;
    // The step being previewed, so the attachments listed are the ones that step
    // sends (campaign-wide files plus its own). Without it, campaign-wide only.
    step_id?: string;
    // Applies that mailbox's signature and reports it as the sender.
    account_id?: string;
}

// Renders a campaign template exactly as the send path does (Go template +
// spintax, then signature and opt-out footer when the campaign and mailbox are
// given), returning the output + parse errors + tokens that didn't resolve.
export async function previewTemplate(input: TemplatePreviewInput): Promise<TemplatePreview> {
    return await Request<TemplatePreview>({
        method: "POST",
        url: "/campaign-template-preview",
        data: input,
        authorization: true,
    });
}
