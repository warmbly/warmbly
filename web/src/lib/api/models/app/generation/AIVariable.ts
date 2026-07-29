import type { AIVariableMode } from "@/lib/aiVariables";
import type { WriteResponse } from "./Write";

// POST /generation/ai-variable — generates one per-recipient snippet from an AI
// variable's config, for the config popover's "Preview" button. Resolves the
// prompt against `contact_id` when given, otherwise a sample contact. Same
// credits/402 semantics as /generation/write.
export interface AIVariableGenerateRequest {
    mode: AIVariableMode;
    prompt: string;
    tone?: string;
    web_search?: boolean;
    contact_id?: string;
    // The email text on either side of the block, so the generated fragment fits
    // the sentence it lands in (matches the send path).
    context_before?: string;
    context_after?: string;
}

export type AIVariableGenerateResponse = WriteResponse;
