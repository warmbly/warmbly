export default interface Template {
    id: string;
    organization_id: string;
    user_id: string;
    name: string;
    subject: string;
    body_html: string;
    body_plain: string;
    position: number;
    created_at: Date;
    updated_at: Date;
}

export interface TemplatesResult {
    data: Template[];
}

export interface CreateTemplateInput {
    name: string;
    subject?: string;
    body_html?: string;
    body_plain?: string;
}

export interface UpdateTemplateInput {
    name?: string;
    subject?: string;
    body_html?: string;
    body_plain?: string;
}

export interface RenderedTemplate {
    subject: string;
    body_html: string;
    body_plain: string;
}
