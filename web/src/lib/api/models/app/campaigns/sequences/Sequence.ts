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

    // Conditional step routing. Absent when the step has no branches; a PATCH
    // with this field replaces the step's branch set wholesale.
    conditions?: SequenceConditions | null;

    // "email" (default — subject/body are sent) or "action" (a side effect named
    // by action.type — no email is sent). Step spacing is the per-step wait_after,
    // not a node kind.
    kind: "email" | "action";
    // Typed config for action nodes; empty/absent for email nodes.
    action?: SequenceAction | null;

    updated_at: Date;
    created_at: Date;
}
