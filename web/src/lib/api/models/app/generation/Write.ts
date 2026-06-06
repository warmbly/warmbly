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
    model: string;
}

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
