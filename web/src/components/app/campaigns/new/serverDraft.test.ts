import { beforeEach, describe, expect, it, vi } from "vitest";
import type Sequence from "@/lib/api/models/app/campaigns/sequences/Sequence";

const calls: string[] = [];
const api = {
    getCampaign: vi.fn(),
    getSequences: vi.fn(),
    listCampaignSegments: vi.fn(),
    setCampaignSegments: vi.fn(async (id: string, ids: string[]) => {
        calls.push(`segments ${id} ${ids.join(",")}`);
        return { data: [], added: 0, withdrawn: 0, contacted: 0 };
    }),
    updateCampaign: vi.fn(async (id: string) => {
        calls.push(`campaign ${id}`);
        return {};
    }),
    createCampaign: vi.fn(async () => ({ id: "new-campaign" })),
    createSequence: vi.fn(async () => {
        calls.push("create step");
        return { id: "s-new" };
    }),
    updateSequence: vi.fn(async (_c: string, sid: string, data: Partial<Sequence>) => {
        calls.push(`update ${sid}${data.conditions ? ` -> ${data.conditions.branches[0]?.target_step_id ?? "end"}` : ""}${data.subject !== undefined ? " content" : ""}`);
        return {};
    }),
    deleteSequence: vi.fn(async (_c: string, sid: string) => {
        calls.push(`delete ${sid}`);
    }),
};

vi.mock("@/lib/api/client/app/campaigns/getCampaign", () => ({ default: (id: string) => api.getCampaign(id) }));
vi.mock("@/lib/api/client/app/campaigns/createCampaign", () => ({ default: () => api.createCampaign() }));
vi.mock("@/lib/api/client/app/campaigns/updateCampaign", () => ({ default: (id: string) => api.updateCampaign(id) }));
vi.mock("@/lib/api/client/app/campaigns/sequences/getSequences", () => ({ default: (id: string) => api.getSequences(id) }));
vi.mock("@/lib/api/client/app/campaigns/sequences/createSequence", () => ({ default: () => api.createSequence() }));
vi.mock("@/lib/api/client/app/campaigns/sequences/updateSequence", () => ({
    default: (c: string, sid: string, data: Partial<Sequence>) => api.updateSequence(c, sid, data),
}));
vi.mock("@/lib/api/client/app/campaigns/sequences/deleteSequence", () => ({
    default: (c: string, sid: string) => api.deleteSequence(c, sid),
}));
vi.mock("@/lib/api/client/app/campaigns/estimateCampaign", () => ({
    default: async () => ({ recipients: 7 }),
}));
vi.mock("@/lib/api/client/app/segments", () => ({
    listCampaignSegments: (id: string) => api.listCampaignSegments(id),
    setCampaignSegments: (id: string, ids: string[]) => api.setCampaignSegments(id, ids),
}));

import { draftSignature, loadServerDraft, saveServerDraft } from "./serverDraft";

function step(id: string, subject: string, next: string | null, extra: Partial<Sequence> = {}): Sequence {
    return {
        id,
        name: "Step",
        subject,
        body_plain: `${subject} body`,
        body_html: `<p>${subject} body</p>`,
        body_sync: true,
        body_code: false,
        wait_after: 3,
        thread_reply: true,
        x: 0,
        y: 0,
        kind: "email",
        // As the API answers: an unconditional connection carries no `conditions`.
        conditions: next ? ({ branches: [{ branch_id: `b-${id}`, target_step_id: next }] } as unknown as Sequence["conditions"]) : null,
        updated_at: new Date(),
        created_at: new Date(),
        ...extra,
    };
}

const campaign = {
    id: "c1",
    name: "Founders",
    description: "",
    status: "draft",
    timezone: "",
    days: 31,
    start_time: "08:00:00.000000",
    end_time: "18:00:00.000000",
    schedule_windows: [null, null, null, null, null, null, null],
    email_tags: [],
    sender_strategy: "tags",
    daily_limit: 50,
    stop_on_reply: true,
    stop_on_meeting: true,
    open_tracking: true,
    link_tracking: true,
    utm_tracking: true,
    unsubscribe_header: true,
    start_date: null,
};

beforeEach(() => {
    calls.length = 0;
    vi.clearAllMocks();
    api.getCampaign.mockResolvedValue(campaign);
    api.listCampaignSegments.mockResolvedValue([{ segment_id: "seg-1" }]);
});

describe("server drafts", () => {
    it("reads a straight chain as editable emails", async () => {
        api.getSequences.mockResolvedValue([step("s1", "Hi", "s2"), step("s2", "", null)]);
        const { draft, meta } = await loadServerDraft("c1");
        expect(meta.stepsLocked).toBe(false);
        expect(draft.emails.map((e) => e.serverId)).toEqual(["s1", "s2"]);
        expect(draft.startTime).toBe("08:00");
        expect(draft.segmentIds).toEqual(["seg-1"]);
    });

    it("rewires the chain before deleting a removed step", async () => {
        api.getSequences.mockResolvedValue([step("s1", "Hi", "s2"), step("s2", "Bump", "s3"), step("s3", "Last", null)]);
        const loaded = await loadServerDraft("c1");
        const edited = {
            ...loaded.draft,
            emails: [loaded.draft.emails[0], loaded.draft.emails[2], { ...loaded.draft.emails[2], id: "local", serverId: undefined, subject: "New" }],
        };
        await saveServerDraft(edited, "Founders", { id: "c1", meta: loaded.meta });
        expect(calls).toEqual([
            "campaign c1",
            "create step",
            "update s1 -> s3 content",
            "update s3 -> s-new",
            "update s-new content",
            "delete s2",
        ]);
    });

    it("keeps step names given on the Steps tab", async () => {
        api.getSequences.mockResolvedValue([step("s1", "Hi", null, { name: "Intro", wait_after: 0 })]);
        const loaded = await loadServerDraft("c1");
        const edited = { ...loaded.draft, emails: [{ ...loaded.draft.emails[0], subject: "Hello" }] };
        await saveServerDraft(edited, "Founders", { id: "c1", meta: loaded.meta });
        expect(api.updateSequence).toHaveBeenCalledWith("c1", "s1", expect.objectContaining({ name: "Intro", subject: "Hello" }));
        expect(loaded.meta.leadCount).toBe(7);
    });

    it("hands back a new campaign's id before a later call can fail", async () => {
        api.setCampaignSegments.mockRejectedValueOnce(new Error("boom"));
        const created: string[] = [];
        const { draft } = await loadServerDraft("c1");
        await expect(saveServerDraft(draft, "Founders", null, (id) => created.push(id))).rejects.toThrow("boom");
        expect(created).toEqual(["new-campaign"]);
    });

    it("renumbers automatic step names when a step moves up", async () => {
        api.getSequences.mockResolvedValue([step("s1", "Hi", "s2", { name: "Step 1", wait_after: 0 }), step("s2", "Bump", "s3", { name: "Step 2" }), step("s3", "Last", null, { name: "Step 3" })]);
        const loaded = await loadServerDraft("c1");
        await saveServerDraft({ ...loaded.draft, emails: [loaded.draft.emails[0], loaded.draft.emails[2]] }, "Founders", { id: "c1", meta: loaded.meta });
        expect(api.updateSequence).toHaveBeenCalledWith("c1", "s3", expect.objectContaining({ name: "Step 2" }));
    });

    it("keeps a branch added on the Steps tab after the flow opened", async () => {
        api.getSequences.mockResolvedValueOnce([step("s1", "Hi", null)]);
        const loaded = await loadServerDraft("c1");
        api.getSequences.mockResolvedValueOnce([
            step("s1", "Hi", "s2", { conditions: { branches: [{ branch_id: "b", target_step_id: "s2", conditions: [{ field: "opened", operator: "ever" }] }] } }),
            step("s2", "Opened", null),
        ]);
        await saveServerDraft({ ...loaded.draft, emails: [{ ...loaded.draft.emails[0], subject: "Changed" }] }, "Founders", { id: "c1", meta: loaded.meta });
        expect(calls).toEqual(["campaign c1"]);
    });

    it("leaves the steps alone when a follow-up was added elsewhere", async () => {
        api.getSequences.mockResolvedValueOnce([step("s1", "Hi", null)]);
        const loaded = await loadServerDraft("c1");
        api.getSequences.mockResolvedValueOnce([step("s1", "Hi", "s9"), step("s9", "Added on the Steps tab", null)]);
        await saveServerDraft({ ...loaded.draft, emails: [{ ...loaded.draft.emails[0], subject: "Changed" }] }, "Founders", { id: "c1", meta: loaded.meta });
        expect(calls).toEqual(["campaign c1"]);
    });

    it("remembers the steps it created, so a retry keeps rewriting them", async () => {
        api.getSequences.mockResolvedValueOnce([step("s1", "Hi", null)]);
        const loaded = await loadServerDraft("c1");
        api.getSequences.mockResolvedValueOnce([step("s1", "Hi", null)]);
        const edited = { ...loaded.draft, emails: [loaded.draft.emails[0], { ...loaded.draft.emails[0], id: "local", serverId: undefined, subject: "More" }] };
        await saveServerDraft(edited, "Founders", { id: "c1", meta: loaded.meta });
        expect(loaded.meta.knownStepIds).toContain("s-new");
    });

    it("leaves an unchanged chain and its lists alone", async () => {
        api.getSequences.mockResolvedValue([step("s1", "Hi", "s2", { name: "Step 1", wait_after: 0 }), step("s2", "Bump", null, { name: "Step 2" })]);
        const loaded = await loadServerDraft("c1");
        await saveServerDraft(loaded.draft, "Founders", { id: "c1", meta: loaded.meta });
        expect(calls).toEqual(["campaign c1"]);
    });

    it("never touches a flow with branches", async () => {
        const branched = step("s1", "Hi", "s2", {
            conditions: {
                branches: [
                    { branch_id: "b1", target_step_id: "s2", conditions: [{ field: "opened", operator: "ever" }] },
                    { branch_id: "b2", target_step_id: null, conditions: [] },
                ],
            },
        });
        api.getSequences.mockResolvedValue([branched, step("s2", "Bump", null)]);
        const loaded = await loadServerDraft("c1");
        expect(loaded.meta.stepsLocked).toBe(true);
        await saveServerDraft(loaded.draft, "Founders", { id: "c1", meta: loaded.meta });
        expect(calls).toEqual(["campaign c1"]);
    });

    it("signs a draft by its content, not its local ids", async () => {
        api.getSequences.mockResolvedValue([step("s1", "Hi", null)]);
        const { draft } = await loadServerDraft("c1");
        expect(draftSignature({ ...draft, emails: draft.emails.map((e) => ({ ...e, id: "other" })) })).toBe(draftSignature(draft));
    });
});
