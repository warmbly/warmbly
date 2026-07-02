import type { SequenceConditions } from "./Branching";
import type { SequenceAction } from "./Action";

export default interface Sequence {
    id: string;
    name: string;

    subject: string;

    body_plain: string;
    body_html: string;
    body_sync: boolean;
    body_code: boolean;

    wait_after: number;

    // Persisted canvas coordinates in the sequence builder. 0/0 means "not
    // placed yet" (the editor auto-arranges until a step is first dragged).
    // Written only through the layout endpoint, never a content PATCH.
    x: number;
    y: number;

    // Conditional step routing. Absent when the step has no branches; a PATCH
    // with this field replaces the step's branch set wholesale.
    conditions?: SequenceConditions | null;

    // "email" (default — subject/body are sent), "action" (a side effect named
    // by action.type — no email is sent), or "wait" (a no-op control node used
    // as a Condition / pure router in the flow editor). Step spacing is the
    // per-step wait_after, not a node kind.
    kind: "email" | "action" | "wait";
    // Typed config for action nodes; empty/absent for email nodes.
    action?: SequenceAction | null;

    updated_at: Date;
    created_at: Date;
}
