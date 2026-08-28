// The workspace's sending posture. The raw detector evidence is deliberately
// not exposed: it is operator-facing, and handing an actor the exact weights
// that flagged them is a map for evading the next check.

export type OrgRiskState = "trusted" | "watch" | "restricted" | "suspended";

export interface OrgRisk {
    state: OrgRiskState;
    restricted: boolean;
    suspended: boolean;
    reason?: string;
}
