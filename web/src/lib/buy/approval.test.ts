import { describe, expect, it } from "vitest";
import type { Order, StoreMailbox } from "@/lib/api/models/app/store/Store";
import { approvalOutcome, waitingForApproval } from "./approval";

const box = (id: string, email: string, domainId: string, action: StoreMailbox["action_required"]): StoreMailbox =>
    ({ id, email, domain_id: domainId, action_required: action, connect_state: action ? "waiting" : "connected" }) as StoreMailbox;

describe("waitingForApproval", () => {
    it("groups the waiting mailboxes by domain and names each domain", () => {
        const order = {
            domains: [{ id: "d1", name: "getacme.com" }],
            mailboxes: [
                box("1", "ada@getacme.com", "d1", "microsoft_consent"),
                box("2", "bob@getacme.com", "d1", "microsoft_consent"),
                box("3", "cy@getacme.com", "d1", null),
                box("4", "dee@other.com", "d2", "microsoft_consent"),
            ],
        } as unknown as Order;
        expect(waitingForApproval(order)).toEqual([
            { domainId: "d1", domain: "getacme.com", count: 2 },
            { domainId: "d2", domain: "other.com", count: 1 },
        ]);
    });
});

describe("approvalOutcome", () => {
    it("reads the status the return carries", () => {
        expect(approvalOutcome("connected", "getacme.com")?.kind).toBe("success");
        expect(approvalOutcome("cancelled", "getacme.com")?.kind).toBe("info");
        expect(approvalOutcome("error", null)?.text).toContain("the domain");
        expect(approvalOutcome(null, "getacme.com")).toBeNull();
        expect(approvalOutcome("anything", "getacme.com")).toBeNull();
    });
});
