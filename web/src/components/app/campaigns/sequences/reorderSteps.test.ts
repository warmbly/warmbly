import { describe, expect, it } from "vitest";
import type Sequence from "@/lib/api/models/app/campaigns/sequences/Sequence";
import type { SequenceBranch } from "@/lib/api/models/app/campaigns/sequences/Branching";
import { firstEmailHasSubject, isLinearChain, reorderLinear } from "./campaignGraph";

const to = (id: string, target: string | null, conditions: SequenceBranch["conditions"] = []): SequenceBranch => ({ branch_id: id, target_step_id: target, conditions });
const step = (id: string, subject: string, wait: number, branches: SequenceBranch[] = []) =>
    ({ id, name: id, subject, wait_after: wait, kind: "email", thread_reply: subject === "", conditions: { branches } }) as unknown as Sequence;

describe("reorderLinear", () => {
    it("keeps each position's wait and connects the steps in the new order", () => {
        const steps = [step("a", "Intro", 0, [to("ab", "b")]), step("b", "Bump", 3, [to("bc", "c")]), step("c", "", 4)];
        const out = reorderLinear(steps, ["b", "a", "c"]);
        expect(out.map((s) => [s.id, s.wait_after, s.conditions?.branches?.map((b) => b.target_step_id)])).toEqual([
            ["b", 0, ["a"]],
            ["a", 3, ["c"]],
            ["c", 4, []],
        ]);
        expect(isLinearChain(out)).toBe(true);
    });
});

describe("isLinearChain", () => {
    it("rejects a condition, a detached step and a jump", () => {
        const opened = [{ field: "opened" as const, operator: "ever" as const }] as SequenceBranch["conditions"];
        expect(isLinearChain([step("a", "x", 0, [to("1", "b", opened), to("2", "b")]), step("b", "y", 3)])).toBe(false);
        expect(isLinearChain([step("a", "x", 0), step("b", "y", 3)])).toBe(false);
        expect(isLinearChain([step("a", "x", 0, [to("1", "c")]), step("b", "y", 3, [to("2", "c")]), step("c", "z", 4)])).toBe(false);
    });
});

describe("firstEmailHasSubject", () => {
    it("needs a subject on the first email", () => {
        expect(firstEmailHasSubject([step("b", "", 3), step("a", "Intro", 0)])).toBe(false);
        expect(firstEmailHasSubject([step("a", "Intro", 0), step("b", "", 3)])).toBe(true);
    });
});
