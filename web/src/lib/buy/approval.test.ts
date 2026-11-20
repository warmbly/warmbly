import { describe, expect, it } from "vitest";
import type { Order, StoreMailbox } from "@/lib/api/models/app/store/Store";
import { approvalOutcome, approvalReason, waitingForApproval } from "./approval";

const box = (id: string, email: string, domainId: string, action: StoreMailbox["action_required"], reason: StoreMailbox["action_reason"] = null): StoreMailbox =>
    ({ id, email, domain_id: domainId, action_required: action, action_reason: reason, connect_state: action ? "waiting" : "connected" }) as StoreMailbox;

describe("waitingForApproval", () => {
    it("groups the waiting mailboxes by domain and names each domain", () => {
        const order = {
            domains: [{ id: "d1", name: "getacme.com" }],
            mailboxes: [
                box("1", "ada@getacme.com", "d1", "microsoft_consent"),
                box("2", "bob@getacme.com", "d1", "microsoft_consent", "mfa_required"),
                box("3", "cy@getacme.com", "d1", null),
                box("4", "dee@other.com", "d2", "microsoft_consent"),
            ],
        } as unknown as Order;
        expect(waitingForApproval(order)).toEqual([
            { domainId: "d1", domain: "getacme.com", count: 2, reason: "mfa_required" },
            { domainId: "d2", domain: "other.com", count: 1, reason: null },
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

describe("approvalReason", () => {
    it("says in one line why the background approval stopped", () => {
        expect(approvalReason("mfa_required", "getacme.com", "admin@getacme.com")).toBe("Microsoft asked for an extra sign-in step for admin@getacme.com.");
        expect(approvalReason("wrong_password", "getacme.com", undefined)).toContain("the admin mailbox of getacme.com");
        expect(approvalReason(null, "getacme.com", "admin@getacme.com")).toContain("getacme.com");
    });
});
