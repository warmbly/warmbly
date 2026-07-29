// AI variables — addable email blocks that generate unique copy for EACH
// recipient at send time. The whole config rides inline in the editor HTML so
// no separate storage/registry is needed: the node serializes as
//   <span data-ai-var="ID" data-ai-config="BASE64_UTF8_JSON">[[ai:ID]]</span>
// The control-plane resolver (internal/tasks/ai_variables.go) reads the config,
// renders the prompt against the contact, calls the model, then replaces the
// whole span (body_html) / the [[ai:ID]] token (body_plain, subject) with the
// generated text, so the config never ships to the recipient. The token carries
// no {{ }} or | so it passes through the Go template renderer and spintax
// untouched.

export type AIVariableMode = "instant" | "research";

export interface AIVariableConfig {
    name: string; // short label shown on the chip, e.g. "opener"
    mode: AIVariableMode; // instant = prompt + history; research = agentic web research
    prompt: string; // a Go-template string, rendered per contact before the model call
    tone: string; // one of WRITE_TONES values ("" = model default)
    web_search: boolean; // allow one bounded web lookup (instant mode)
}

export const DEFAULT_AI_CONFIG: AIVariableConfig = {
    name: "",
    mode: "instant",
    prompt: "",
    tone: "",
    web_search: false,
};

// aiToken is the plain-text marker the resolver substitutes per recipient.
export function aiToken(id: string): string {
    return `[[ai:${id}]]`;
}

// newAIVariableId mints a fresh id for a block. crypto.randomUUID is available in
// every browser the dashboard targets.
export function newAIVariableId(): string {
    if (typeof crypto !== "undefined" && crypto.randomUUID) return crypto.randomUUID();
    return `ai-${Math.random().toString(36).slice(2)}${Date.now().toString(36)}`;
}

// encodeConfig / decodeConfig round-trip the config through UTF-8-safe base64 so
// it survives inside an HTML attribute and is opaque to the Go template renderer.
export function encodeConfig(cfg: AIVariableConfig): string {
    const json = JSON.stringify(cfg);
    const bytes = new TextEncoder().encode(json);
    let bin = "";
    for (const b of bytes) bin += String.fromCharCode(b);
    return btoa(bin);
}

export function decodeConfig(b64: string): AIVariableConfig {
    try {
        const bin = atob(b64);
        const bytes = Uint8Array.from(bin, (c) => c.charCodeAt(0));
        const json = new TextDecoder().decode(bytes);
        // Force instant: the research mode was removed from the product, so any
        // block persisted with mode:"research" normalizes back to instant on load
        // (and re-serializes clean the next time the editor writes its HTML).
        return { ...DEFAULT_AI_CONFIG, ...(JSON.parse(json) as Partial<AIVariableConfig>), mode: "instant" };
    } catch {
        return { ...DEFAULT_AI_CONFIG };
    }
}
