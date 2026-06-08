import Request from "../../Request";

export interface TemplatePreview {
    subject: string;
    body_html: string;
    body_plain: string;
    errors?: string[]; // template parse errors (these block sending)
    unresolved?: string[]; // literal {{…}} tokens left after render
}

export interface TemplatePreviewInput {
    subject: string;
    body_html: string;
    body_plain: string;
}

// Renders a campaign template against a sample contact exactly as the send path
// does (Go template + spintax), returning the output + parse errors + tokens
// that didn't resolve.
export async function previewTemplate(input: TemplatePreviewInput): Promise<TemplatePreview> {
    return await Request<TemplatePreview>({
        method: "POST",
        url: "/campaign-template-preview",
        data: input,
        authorization: true,
    });
}
