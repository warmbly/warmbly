import type { IntegrationAction } from "@/lib/api/models/app/integrations/Integration";

// One action node of an automation: run `action` on `connection_id` with config.
export interface AutomationStep {
    id?: string;
    connection_id: string;
    action: IntegrationAction;
    config?: Record<string, unknown>;
}

// An automation = one trigger event -> a set of action steps. Execution reuses
// the integration event-subscription dispatcher (each step is a subscription).
export interface Automation {
    id: string;
    organization_id: string;
    name: string;
    enabled: boolean;
    trigger_event: string;
    filter?: Record<string, unknown>;
    created_at: string;
    updated_at: string;
    steps: AutomationStep[];
}

// Create/update payload from the flow builder.
export interface AutomationWrite {
    name: string;
    enabled: boolean;
    trigger_event: string;
    filter?: Record<string, unknown>;
    steps: AutomationStep[];
}
