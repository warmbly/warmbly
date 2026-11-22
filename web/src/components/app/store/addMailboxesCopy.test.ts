import { describe, expect, it } from "vitest";
import { addMailboxesCopy } from "./addMailboxesCopy";
import type { PartnerManaged } from "@/lib/api/models/app/organizations/Organization";

const graphkit: PartnerManaged = { display_name: "GraphKit", state: "active" };

describe("addMailboxesCopy", () => {
    it("names the reseller and never the store in a managed workspace", () => {
        const copy = addMailboxesCopy({ partner: graphkit, storeOn: true, writable: true });
        expect(copy.body).toContain("added to this workspace through GraphKit");
        expect(copy.body).toContain("stays off until GraphKit turns it on");
        expect(copy.body).not.toMatch(/buy/i);
        expect(copy.connectLabel).toBe("Connect a mailbox");
    });

    it("offers no connect while the managed workspace is read-only", () => {
        const copy = addMailboxesCopy({ partner: { ...graphkit, state: "suspended" }, storeOn: false, writable: false });
        expect(copy.body).toBe("Mailboxes are added to this workspace through GraphKit.");
        expect(copy.connectLabel).toBeNull();
    });

    it("leads with buying only where the store is on", () => {
        expect(addMailboxesCopy({ partner: null, storeOn: true, writable: true }).body).toMatch(/^Buy a domain/);
        const off = addMailboxesCopy({ partner: null, storeOn: false, writable: true });
        expect(off.body).not.toMatch(/buy/i);
        expect(off.connectLabel).toBe("Add account");
    });
});
