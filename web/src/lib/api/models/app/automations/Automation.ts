import type { IntegrationAction } from "@/lib/api/models/app/integrations/Integration";

// A condition (IF) tested against the trigger event's data. For the generic
// "field" type, `key` names the event-data key to test. For the "expression"
// type, `expression` is a free-form Go-template predicate (truthy = pass).
export interface AutomationCondition {
    field: string;
    key?: string;
    operator: string;
    value?: unknown;
    expression?: string;
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
    when?: "" | "true" | "false" | "error";
}

export interface AutomationGraph {
    nodes: AutomationNode[];
    edges: AutomationEdge[];
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
