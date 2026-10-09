import { describe, expect, it } from "vitest";
import { ALL_CAMPAIGNS, isScoped, scopeLabel } from "./campaignScope";

const folderName = (id: string) => ({ f1: "Client A", f2: "Client B" })[id];
const campaignName = (id: string) => ({ c1: "Dentists" })[id];

describe("campaign scope label", () => {
    it("reads all campaigns when nothing is selected", () => {
        expect(isScoped(ALL_CAMPAIGNS)).toBe(false);
        expect(scopeLabel(ALL_CAMPAIGNS, undefined, folderName, campaignName)).toBe("All campaigns");
    });

    it("names a folder with the campaigns the server resolved", () => {
        const filter = { campaigns: [], folders: ["f1"] };
        expect(scopeLabel(filter, undefined, folderName, campaignName)).toBe("Client A");
        const scope = { campaigns: [], folders: [{ id: "f1", name: "Client A" }], campaign_count: 6 };
        expect(scopeLabel(filter, scope, folderName, campaignName)).toBe("Client A · 6 campaigns");
    });

    it("names one campaign, and counts several", () => {
        expect(scopeLabel({ campaigns: ["c1"], folders: [] }, undefined, folderName, campaignName)).toBe("Dentists");
        expect(scopeLabel({ campaigns: ["c1", "c2", "c3"], folders: [] }, undefined, folderName, campaignName)).toBe("3 campaigns");
    });

    it("counts a mix once, by the server's deduplicated total", () => {
        const filter = { campaigns: ["c1"], folders: ["f1", "f2"] };
        const scope = { campaigns: [{ id: "c1", name: "Dentists" }], folders: [], campaign_count: 1 };
        expect(scopeLabel(filter, scope, folderName, campaignName)).toBe("2 folders + 1 campaign · 1 campaign");
    });
});
