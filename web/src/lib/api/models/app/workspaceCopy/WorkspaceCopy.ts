// The answer to a copy-to-workspace request: one entry per destination.

export type WorkspaceCopyStatus = "copied" | "partial" | "failed";

export interface CopiedRef {
    id: string;
    name: string;
}

// A copied step whose settings named something only the source workspace has.
export interface CopyReview {
    step_id: string;
    step_name: string;
    fields: string[];
}

export interface WorkspaceCopyResult {
    organization_id: string;
    organization_name: string;
    status: WorkspaceCopyStatus;
    campaign?: CopiedRef;
    templates: CopiedRef[];
    review: CopyReview[];
    warnings: string[];
    error?: { code: string; message: string };
}

export interface WorkspaceCopyResponse {
    data: WorkspaceCopyResult[];
}

// Mirrors models.CopyToWorkspacesMax and models.CopyTemplatesMax.
export const COPY_DESTINATIONS_MAX = 10;
export const COPY_TEMPLATES_MAX = 50;
