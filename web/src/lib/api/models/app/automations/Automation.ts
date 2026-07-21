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
    type: "trigger" | "condition" | "action";
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
// stays forward/back-compatible. AIStepConfig is the unified AI step
// (warmbly.ai_step); its mode selects behavior and, in agent mode, the model
// may call the reversible actions in allowed_actions[]. AISwitchConfig is the
// AI router (warmbly.ai_switch), decided over cases[] and routed by
// "label:<case>" edges (the same edge vocabulary an AI classify node uses).
export type AIStepMode = "classify" | "extract" | "generate" | "decide" | "agent";

export interface AIStepConfig {
    mode: AIStepMode;
    instruction: string;
    output_key?: string;
    labels?: string[];
    output_keys?: string[];
    cases?: string[];
    allowed_actions?: NativeActionAllow[];
}

export interface AISwitchConfig {
    instruction: string;
    cases: string[];
    output_key?: string;
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
