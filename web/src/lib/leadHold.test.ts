import { describe, expect, it } from "vitest";
import type ContactCampaignState from "@/lib/api/models/app/contacts/ContactCampaignState";
import { FOLLOW_UP_PAUSES, endOfLocalDay, followUpPauseUntil, leadCanBePaused, localDayISO } from "./leadHold";

function state(over: Partial<ContactCampaignState>): ContactCampaignState {
    return {
        campaign_id: "c1",
        campaign_name: "Q3 outreach",
        campaign_status: "active",
        lead_status: "active",
        steps: [],
        completed_steps: 1,
        total_steps: 3,
        next: { step_id: "s2", step_label: "Email 2", state: "waiting" },
        ...over,
    };
}

describe("leadCanBePaused", () => {
    it("offers a pause while the flow can still send", () => {
        expect(leadCanBePaused(state({}))).toBe(true);
        expect(leadCanBePaused(state({ lead_status: "pending", campaign_status: "draft" }))).toBe(true);
    });
    it("offers it on a replied lead the router keeps sending to", () => {
        expect(leadCanBePaused(state({ lead_status: "replied" }))).toBe(true);
        expect(leadCanBePaused(state({ lead_status: "replied", ended_reason: "Replied, sending stopped" }))).toBe(false);
    });
    it("refuses a lead that is already held", () => {
        expect(
            leadCanBePaused(state({ lead_status: "paused", hold: { since: "2026-09-01T00:00:00Z", source: "manual" } })),
        ).toBe(false);
    });
    it("refuses ended leads, finished campaigns and an unknown next step", () => {
        expect(leadCanBePaused(state({ next: null, ended_reason: "Every step has been sent" }))).toBe(false);
        expect(leadCanBePaused(state({ lead_status: "replied", next: null }))).toBe(false);
        expect(leadCanBePaused(state({ campaign_status: "completed" }))).toBe(false);
    });
});

describe("hold dates", () => {
    it("counts days from the local date", () => {
        expect(localDayISO(7, new Date(2026, 8, 28, 22, 0))).toBe("2026-10-05");
    });
    it("ends a hold at the end of the chosen local day", () => {
        const end = new Date(endOfLocalDay("2026-10-05") as string);
        expect([end.getFullYear(), end.getMonth(), end.getDate(), end.getHours(), end.getMinutes()]).toEqual([
            2026, 9, 5, 23, 59,
        ]);
        expect(endOfLocalDay("nope")).toBeNull();
    });
    it("counts a follow-up pause from when the reply goes out", () => {
        const week = FOLLOW_UP_PAUSES.find((p) => p.days === 7)!;
        expect(followUpPauseUntil(week, new Date(2026, 9, 10, 9, 0))).toBe(endOfLocalDay("2026-10-17"));
        expect(followUpPauseUntil(FOLLOW_UP_PAUSES.find((p) => p.days === null)!)).toBeNull();
    });
});
