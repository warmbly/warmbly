// Contact research types, mirroring internal/models/research.go.

export type ResearchStatus =
    | "queued"
    | "running"
    | "succeeded"
    | "failed"
    | "nothing_found";

export interface ResearchArtifact {
    what: string;
    where?: string;
    when?: string;
    url: string;
}

export interface ResearchSignal {
    type: string;
    fact: string;
    when?: string;
    url: string;
    confidence: "high" | "medium" | "low";
}

export interface ResearchHook {
    based_on: string;
    why_relevant?: string;
    opener_line: string;
}

export interface ResearchResult {
    company?: {
        summary?: string;
        industry?: string;
        size_estimate?: string;
        sells_to?: string;
        tech_or_stack_signals?: string[];
    };
    person?: {
        role_confirmed: boolean;
        title?: string;
        public_artifacts?: ResearchArtifact[];
    };
    signals?: ResearchSignal[];
    hooks?: ResearchHook[];
    custom_field_updates?: Record<string, string>;
    research_notes?: string;
    nothing_found: boolean;
}

export interface ContactResearchRun {
    id: string;
    org_id: string;
    contact_id: string;
    requested_by?: string;
    status: ResearchStatus;
    objective: string;
    result: ResearchResult;
    error?: string;
    credits_charged: number;
    model_used: string;
    tokens_used: number;
    created_at: string;
    updated_at: string;
}
