// A reorder must rewire each step's catch-all to the step that follows it, the
// way the server does on save. Reordering the array alone leaves the canvas
// drawing the old chain (and can warn that steps are detached) until a refetch.

import { describe, expect, it } from "vitest";
import type Sequence from "@/lib/api/models/app/campaigns/sequences/Sequence";
import type { SequenceBranch } from "@/lib/api/models/app/campaigns/sequences/Branching";
import { catchAll, isCond, reorderSteps } from "./campaignGraph";

const br = (id: string, target: string | null, conditions: SequenceBranch["conditions"] = []): SequenceBranch => ({ branch_id: id, target_step_id: target, conditions });
const chain = (target: string | null): SequenceBranch[] => [br("else", target)];
const opened = [{ field: "opened" as const, operator: "within_days" as const, value: 3 }];

function step(id: string, branches: SequenceBranch[] = []): Sequence {
    return {
        id,
        name: id,
        subject: "",
        body_plain: "",
        body_html: "",
        body_sync: false,
        body_code: false,
        wait_after: 0,
        thread_reply: true,
        x: 0,
        y: 0,
        kind: "email",
        conditions: { branches },
        updated_at: new Date(),
        created_at: new Date(),
    } as Sequence;
}

const order = (steps: Sequence[]) => steps.map((s) => s.id);
const targetOf = (s: Sequence) => catchAll(s)?.target_step_id ?? null;

describe("reorderSteps", () => {
    it("rewires the catch-all chain to follow the new order", () => {
        const steps = [step("a", chain("b")), step("b", chain("c")), step("c", chain(null))];

        const next = reorderSteps(steps, ["c", "a", "b"]);

        expect(order(next)).toEqual(["c", "a", "b"]);
        expect(targetOf(next[0])).toBe("a"); // c -> a
        expect(targetOf(next[1])).toBe("b"); // a -> b
        expect(targetOf(next[2])).toBeNull(); // b ends the sequence
    });

    it("keeps conditional branches pointed at their original target", () => {
        // A: opened -> B (conditional), otherwise -> C (catch-all).
        const steps = [step("a", [br("opened", "b", opened), br("else", "c")]), step("b", chain(null)), step("c", chain(null))];

        const next = reorderSteps(steps, ["c", "a", "b"]);
        const a = next.find((s) => s.id === "a")!;

        const conditional = (a.conditions?.branches ?? []).filter(isCond);
        expect(conditional).toHaveLength(1);
        expect(conditional[0].target_step_id).toBe("b"); // unchanged
        expect(targetOf(a)).toBe("b"); // rewritten to the step after A
    });

    it("adds a catch-all to a step that had none when it is no longer last", () => {
        const steps = [step("a", chain("b")), step("b")];

        const next = reorderSteps(steps, ["b", "a"]);

        expect(targetOf(next[0])).toBe("a");
        expect(targetOf(next[1])).toBeNull();
    });

    it("leaves steps omitted from the order at the end with a rewired chain", () => {
        const steps = [step("a", chain("b")), step("b", chain("c")), step("c", chain(null))];

        const next = reorderSteps(steps, ["b"]);

        expect(order(next)).toEqual(["b", "a", "c"]);
        expect(targetOf(next[0])).toBe("a");
        expect(targetOf(next[1])).toBe("c");
        expect(targetOf(next[2])).toBeNull();
    });
});
