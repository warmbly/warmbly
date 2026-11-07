// AI writing assistant — POST /generation/write. Drafts email copy from a short
// prompt; the response reports what the call cost, the balance after it and the
// model that produced the text. A 402 means the workspace balance is too low.
export interface WriteRequest {
    prompt: string;
    tone?: string;
}

export interface WriteResponse {
    text: string;
    // What this call cost in dollars; 0 on unmetered (local model) setups.
    charged_usd: number;
    balance_usd: number;
    tokens_used: number;
    model: string;
}

// AI selection edit — POST /generation/edit. Rewrites a passage of a draft
// according to an instruction; `context` optionally carries the full draft for
// tone consistency. Same billing/402 semantics as /generation/write.
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
