// AI writing assistant — POST /generation/write. Drafts email copy from a short
// prompt; the response reports the remaining generation credits and the model
// that produced the text. A 402 means the org is out of credits.
export interface WriteRequest {
    prompt: string;
    tone?: string;
}

export interface WriteResponse {
    text: string;
    credits_remaining: number;
    // Real usage-based charge for this call: flat minimum plus the token
    // overage settle. 0 on unmetered (local model) setups.
    credits_charged: number;
    tokens_used: number;
    model: string;
}

// AI selection edit — POST /generation/edit. Rewrites a passage of a draft
// according to an instruction; `context` optionally carries the full draft for
// tone consistency. Same credits/402 semantics as /generation/write.
export interface EditRequest {
    text: string;
    instruction: string;
    context?: string;
    tone?: string;
}

export type EditResponse = WriteResponse;

// Tone presets surfaced in the "Write with AI" popover. `value` is sent as the
// `tone` field; an empty value lets the backend pick its default.
export const WRITE_TONES: { value: string; label: string }[] = [
    { value: "", label: "Default" },
    { value: "friendly", label: "Friendly" },
    { value: "professional", label: "Professional" },
    { value: "casual", label: "Casual" },
    { value: "concise", label: "Concise" },
    { value: "persuasive", label: "Persuasive" },
];
