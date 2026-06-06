// A file attached to a campaign. When sequence_id is set the attachment is
// scoped to that step; null/absent = campaign-level (attached to every step).
export default interface Attachment {
    id: string;
    filename: string;
    size: number;
    mime_type: string;
    url: string;
    sequence_id?: string | null;
    created_at: Date;
}

// Options for POST /campaigns/:id/attachments. The file rides as the multipart
// "file" field; sequence_id is an optional form field that scopes the upload.
export interface UploadAttachmentOptions {
    sequenceId?: string | null;
}
