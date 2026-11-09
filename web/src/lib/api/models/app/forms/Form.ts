// Hosted lead-capture forms (issue #267). Mirrors internal/models/form.go.

export type FormStatus = "draft" | "published" | "archived";

export type FormFieldType =
    | "text"
    | "email"
    | "phone"
    | "textarea"
    | "number"
    | "select"
    | "radio"
    | "checkboxes"
    | "checkbox"
    | "date"
    | "hidden"
    | "heading"
    | "paragraph"
    | "divider"
    | "page_break";

export interface FormField {
    id: string;
    type: FormFieldType;
    label: string;
    placeholder?: string;
    help_text?: string;
    required: boolean;
    options?: string[];
    /** Contact column the answer fills; empty = contact custom field by label. */
    map_to?: string;
    /** Hidden-field constant or paragraph body. */
    value?: string;
    /** "full" (default) or "half"; two half fields share a row. */
    width?: string;
    rows?: number;
}

export interface FormDesign {
    font_family?: string;
    page_background?: string;
    form_background?: string;
    text_color?: string;
    label_color?: string;
    input_background?: string;
    input_border_color?: string;
    input_text_color?: string;
    placeholder_color?: string;
    accent_color?: string;
    button_background?: string;
    button_text_color?: string;
    button_text?: string;
    button_size?: string;
    button_full_width?: boolean;
    border_radius?: number;
    max_width?: number;
    spacing?: string;
    shadow?: boolean;
    /** Preset id that seeded the current colors; renderers never read it. */
    theme?: string;
    /** "card" (centered box), "wide" (no box) or "split" (cover panel). */
    layout?: string;
    /** "classic" (pages of fields) or "focus" (one question per screen). */
    mode?: string;
    /** Second hex turns the page background into a vertical gradient. */
    page_background_end?: string;
    align?: string;
    show_progress?: boolean;
    /** Image layered over the page color; overlay veils it for legible text. */
    page_background_image?: string;
    /** "cover", "contain" or "tile". */
    background_size?: string;
    /** 0-100. */
    background_overlay?: number;
    header_enabled?: boolean;
    header_title?: string;
    header_background?: string;
    /** "page" spans the viewport, "inline" sits with the form. */
    header_placement?: string;
    /** "left", "center" or "between"; defaults to the form's alignment. */
    header_align?: string;
    header_sticky?: boolean;
    /** false keeps the logo in its own placement and leaves the header the title. */
    header_show_logo?: boolean;
    cover_title?: string;
    cover_subtitle?: string;
    /** "sm" | "md" | "lg". */
    logo_size?: string;
    /** "card" (on the form surface) or "page" (above the card). */
    logo_position?: string;
}

export default interface Form {
    id: string;
    organization_id: string;
    created_by?: string;
    public_id: string;
    name: string;
    status: FormStatus;
    fields: FormField[];
    design: FormDesign;
    success_message: string;
    redirect_url: string;
    /** False when redirect_url is not on one of the workspace's own domains, so visitors see the success message. */
    redirect_url_active?: boolean;
    campaign_id?: string;
    category_ids: string[];
    allowed_domains: string[];
    captcha_enabled: boolean;
    /** Classify each submission through TypeSafe; junk creates no contact. */
    triage_enabled: boolean;
    logo_url: string;
    cover_url: string;
    background_url: string;
    views_count: number;
    submissions_count: number;
    starts_count: number;
    identified_count: number;
    /** Daily submissions for the list sparkline, oldest first; list reads only. */
    trend?: number[];
    last_submission_at?: Date;
    published_at?: Date;
    share_url?: string;
    created_at: Date;
    updated_at: Date;
}

export interface FormWrite {
    name?: string;
    status?: FormStatus;
    fields?: FormField[];
    design?: FormDesign;
    success_message?: string;
    redirect_url?: string;
    campaign_id?: string | null;
    category_ids?: string[];
    allowed_domains?: string[];
    captcha_enabled?: boolean;
    triage_enabled?: boolean;
}

/** The triage verdict on a submission; empty when it was not triaged. */
export type FormTriage = "" | "buyer" | "vendor" | "job_seeker" | "other" | "junk";

export interface FormSubmission {
    id: string;
    form_id: string;
    organization_id: string;
    contact_id?: string;
    /** Campaign whose email carried the personalized link, when known. */
    campaign_id?: string;
    data: Record<string, string | string[]>;
    source_url: string;
    triage?: FormTriage;
    triage_confidence?: number;
    /** RFC 3339 with microseconds; the paging order, so never round it through Date. */
    created_at: string;
    contact_email?: string;
    contact_name?: string;
    campaign_name?: string;
}

export interface FormsConfig {
    base_url: string;
    captcha_available: boolean;
    triage_available: boolean;
}

/** Field types that collect a value on submit. */
export function isInputType(t: FormFieldType): boolean {
    return t !== "heading" && t !== "paragraph" && t !== "divider" && t !== "page_break";
}

/** Field types that need an options list. */
export function hasOptions(t: FormFieldType): boolean {
    return t === "select" || t === "radio" || t === "checkboxes";
}

export const FORM_CONTACT_COLUMNS: { value: string; label: string }[] = [
    { value: "", label: "Custom field (by label)" },
    { value: "first_name", label: "First name" },
    { value: "last_name", label: "Last name" },
    { value: "email", label: "Email" },
    { value: "company", label: "Company" },
    { value: "phone", label: "Phone" },
];
