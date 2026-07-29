import type { IntegrationAction } from "@/lib/api/models/app/integrations/Integration";
import type { NativeActionAllow } from "@/lib/api/models/app/automations/meta";

// A condition (IF) tested against the trigger event's data. For the generic
// "field" type, `key` names the event-data key to test. For the "expression"
// type, `expression` is a free-form Go-template predicate (truthy = pass).
export interface AutomationCondition {
    field: string;
    key?: string;
    operator: string;
    value?: unknown;
    expression?: string;
    // "ai" field: the plain-language yes/no question the model answers over the
    // event data (true edge = yes). Costs 1 credit per evaluation.
    prompt?: string;
}

// One node on the flow canvas. "trigger" is the single entry (id "trigger");
// "condition" is an IF with true/false outgoing edges; "action" runs a provider
// action on a connection.
export interface AutomationNode {
    id: string;
    // "stop" is a terminal marker: a path routed into it ends. It carries no
    // action or condition (mirrors the campaign Stop node).
    type: "trigger" | "condition" | "action" | "stop";
    action?: IntegrationAction;
    connection_id?: string;
    config?: Record<string, unknown>;
    condition?: AutomationCondition;
    x: number;
    y: number;
}

// An edge. `when` is "" for plain edges and "true"/"false" for the two outgoing
// edges of a condition node.
export interface AutomationEdge {
    id: string;
    source: string;
    target: string;
    // "" plain/then, "true"/"false" a condition's branches, "error" an action's
    // on-error branch.
    // "" plain | "true"/"false" (condition branches) | "error" (action on-error)
    // | "label:<x>" (per-label branch out of an AI classify action: followed
    // only when the model picked <x>, so one classify node routes multi-way).
    when?: string;
}

export interface AutomationGraph {
    nodes: AutomationNode[];
    edges: AutomationEdge[];
}

// Read-time parse helpers for the untyped AutomationNode.config blob (jsonb).
// They are tolerant views, never schemas that reject unknown keys, so the blob
// stays forward/back-compatible. These mirror the campaign step types:
// AIStepConfig is the unified AI step (warmbly.ai_step) — a single-shot
// transform (classify/extract/generate) or an agent that calls the reversible
// actions in allowed_actions[], choosing tags/tasks/deals itself. AISwitchConfig
// is the AI router (warmbly.ai_switch), decided over cases[] by an AI prompt or
// a value template and routed by "label:<case>" edges.
export type AIStepMode = "classify" | "extract" | "generate" | "agent";

// One tag/label the agent may use: id + display name, so the backend can offer
// it to the model and resolve its pick without a category lookup.
export interface AITagRef {
    id: string;
    name: string;
}

export interface AIStepConfig {
    mode: AIStepMode;
    instruction: string;
    output_key?: string;
    labels?: string[];
    output_keys?: string[];
    allowed_actions?: NativeActionAllow[];
    // Agent tag/label pools (empty = any of the org's tags); optional create.
    ai_add_tags?: AITagRef[];
    ai_remove_tags?: AITagRef[];
    ai_labels?: AITagRef[];
    ai_allow_create_tags?: boolean;
    // Route any mode to the stronger model tier.
    thinking?: boolean;
}

export interface AISwitchConfig {
    instruction: string;
    cases: string[];
    output_key?: string;
    // "ai" (default) picks a case with one model call; "value" matches
    // switch_value against the case names deterministically (free, no model).
    switch_on?: "ai" | "value";
    switch_value?: string;
    // ai-mode capabilities.
    web_search?: boolean;
    thinking?: boolean;
}

export interface Automation {
    id: string;
    organization_id: string;
    name: string;
    enabled: boolean;
    trigger_event: string;
    filter?: Record<string, unknown>;
    graph: AutomationGraph;
    created_at: string;
    updated_at: string;
    // Public POST path that fires this automation, set only when its trigger is
    // the inbound webhook. Append to the API origin for the full URL.
    inbound_url?: string;
}

export interface AutomationWrite {
    name: string;
    enabled: boolean;
    trigger_event: string;
    filter?: Record<string, unknown>;
    graph: AutomationGraph;
}

// One node's outcome in a run (or a dry-run trace).
export interface AutomationNodeResult {
    node_id: string;
    type: string; // trigger | condition | action
    action?: string;
    label?: string;
    status: string; // success | error | skipped | branch_true | branch_false
    error?: string;
    preview?: Record<string, unknown>; // dry-run only
}

// A persisted execution of an automation graph.
export interface AutomationRun {
    id: string;
    automation_id: string;
    organization_id: string;
    trigger_event: string;
    status: string; // running | success | error
    node_results: AutomationNodeResult[];
    error_detail?: string;
    started_at: string;
    finished_at?: string | null;
}

// Dry-run (test) response: the trace of the walk + the sample data used.
export interface DryRunResponse {
    trace: AutomationNodeResult[];
    data: Record<string, unknown>;
}
