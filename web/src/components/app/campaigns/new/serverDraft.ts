// A draft campaign on the server, read into the new-campaign flow and written
// back from it. The flow edits what it can represent (a straight chain of
// emails, one daily window, tag-based senders); anything richer set on the
// campaign's own tabs is left exactly as it is.

import type Campaign from "@/lib/api/models/app/campaigns/Campaign";
import type Sequence from "@/lib/api/models/app/campaigns/sequences/Sequence";
import getCampaign from "@/lib/api/client/app/campaigns/getCampaign";
import createCampaign from "@/lib/api/client/app/campaigns/createCampaign";
import updateCampaign from "@/lib/api/client/app/campaigns/updateCampaign";
import getSequences from "@/lib/api/client/app/campaigns/sequences/getSequences";
import createSequence from "@/lib/api/client/app/campaigns/sequences/createSequence";
import updateSequence from "@/lib/api/client/app/campaigns/sequences/updateSequence";
import deleteSequence from "@/lib/api/client/app/campaigns/sequences/deleteSequence";
import { listCampaignSegments, setCampaignSegments } from "@/lib/api/client/app/segments";
import estimateCampaign from "@/lib/api/client/app/campaigns/estimateCampaign";
import { htmlToPlain } from "@/components/app/campaigns/sequences/emailPreview";
import { hasContent, initialDraft, scheduledDate, toCreateInput, writtenEmails, type Draft, type EmailDraft } from "./draft";

export type DraftMeta = {
    // Branches, action steps or a reordered flow: the Emails step shows them
    // read-only and the save leaves them alone.
    stepsLocked: boolean;
    // Per-day windows set on the Schedule tab, which one window cannot hold.
    customWindows: boolean;
    // Mailboxes picked one by one rather than by tag.
    explicitSenders: boolean;
    steps: Sequence[];
    segmentIds: string[];
    // Leads on the campaign that no list link enrolled.
    leadCount: number;
    // Steps this flow loaded or created; any other step means someone else edited it.
    knownStepIds: string[];
};

// A campaign just created by the flow: nothing on it is locked yet.
export const freshMeta = (): DraftMeta => ({
    stepsLocked: false,
    customWindows: false,
    explicitSenders: false,
    steps: [],
    segmentIds: [],
    leadCount: 0,
    knownStepIds: [],
});

export type LoadedDraft = { draft: Draft; meta: DraftMeta };

const hhmm = (v: string | undefined, fallback: string) => (v && /^\d{2}:\d{2}/.test(v) ? v.slice(0, 5) : fallback);

// "yyyy-MM-ddTHH:mm" in local time, the DateTimePicker's shape.
function localInput(d: Date): string {
    const p = (n: number) => String(n).padStart(2, "0");
    return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`;
}

// Only plain email steps with at most one unconditional link: nothing a rewrite could lose.
function isRewritable(steps: Sequence[]): boolean {
    return steps.every((s) => {
        const branches = s.conditions?.branches ?? [];
        return (s.kind ?? "email") === "email" && branches.length <= 1 && branches.every((b) => (b.conditions ?? []).length === 0);
    });
}

// A straight chain: every step an email, each connected unconditionally to
// the next in order, the last to nothing.
function isLinear(steps: Sequence[]): boolean {
    return steps.every((s, i) => {
        if ((s.kind ?? "email") !== "email") return false;
        const branches = s.conditions?.branches ?? [];
        // The API omits `conditions` on an unconditional connection.
        const plain = (b: (typeof branches)[number]) => (b.conditions ?? []).length === 0;
        if (i === steps.length - 1) return branches.every((b) => b.target_step_id === null && plain(b));
        return branches.length === 1 && plain(branches[0]) && branches[0].target_step_id === steps[i + 1].id;
    });
}

export async function loadServerDraft(id: string): Promise<LoadedDraft> {
    const [campaign, steps, links, leadCount] = await Promise.all([
        getCampaign(id),
        getSequences(id),
        listCampaignSegments(id),
        estimateCampaign({ segment_ids: [], campaign_id: id }).then((e) => e.recipients, () => 0),
    ]);
    const stepsLocked = !isLinear(steps);
    const customWindows = (campaign.schedule_windows ?? []).some((d) => Array.isArray(d) && d.length > 0);
    const base = initialDraft(campaign.timezone ?? "");
    const start = campaign.start_date ? new Date(campaign.start_date) : null;
    const draft: Draft = {
        ...base,
        name: campaign.name,
        nameTouched: true,
        description: campaign.description ?? "",
        segmentIds: links.map((l) => l.segment_id),
        emails: stepsLocked || steps.length === 0
            ? base.emails
            : steps.map<EmailDraft>((s) => ({
                  id: s.id,
                  serverId: s.id,
                  subject: s.subject ?? "",
                  body_html: s.body_html ?? "",
                  body_plain: s.body_plain ?? "",
                  body_code: !!s.body_code,
                  wait_after: s.wait_after ?? 0,
              })),
        timezone: campaign.timezone ?? "",
        days: campaign.days || base.days,
        startTime: hhmm(campaign.start_time, base.startTime),
        endTime: hhmm(campaign.end_time, base.endTime),
        startMode: start && start.getTime() > Date.now() ? "later" : "now",
        scheduledAt: start && start.getTime() > Date.now() ? localInput(start) : "",
        emailTagIds: campaign.email_tags ?? [],
        dailyLimit: campaign.daily_limit || base.dailyLimit,
        stopOnReply: campaign.stop_on_reply,
        stopOnMeeting: campaign.stop_on_meeting ?? base.stopOnMeeting,
        openTracking: campaign.open_tracking,
        linkTracking: campaign.link_tracking,
        utmTracking: campaign.utm_tracking,
        unsubHeader: campaign.unsubscribe_header,
    };
    return {
        draft,
        meta: {
            stepsLocked,
            customWindows,
            explicitSenders: campaign.sender_strategy === "explicit",
            steps,
            segmentIds: draft.segmentIds,
            leadCount,
            knownStepIds: steps.map((s) => s.id),
        },
    };
}

const sameSet = (a: string[], b: string[]) => a.length === b.length && a.every((x) => b.includes(x));

// Creates the draft or writes the flow back onto it; onCreated lets a failed save retry as an update.
export async function saveServerDraft(
    d: Draft,
    name: string,
    existing: { id: string; meta: DraftMeta } | null,
    onCreated?: (id: string, stepIds: string[]) => void,
): Promise<string> {
    if (!existing) {
        const created = await createCampaign(toCreateInput(d, name));
        onCreated?.(created.id, await getSequences(created.id).then((s) => s.map((x) => x.id), () => []));
        if (d.segmentIds.length > 0) await setCampaignSegments(created.id, d.segmentIds);
        return created.id;
    }
    const { id, meta } = existing;
    const at = scheduledDate(d);
    const patch: Partial<Campaign> = {
        name,
        description: d.description.trim(),
        stop_on_reply: d.stopOnReply,
        stop_on_meeting: d.stopOnMeeting,
        open_tracking: d.openTracking,
        link_tracking: d.linkTracking,
        utm_tracking: d.utmTracking,
        unsubscribe_header: d.unsubHeader,
        daily_limit: d.dailyLimit,
        timezone: d.timezone,
        start_date: at ?? null,
    };
    if (!meta.customWindows) Object.assign(patch, { days: d.days, start_time: d.startTime, end_time: d.endTime });
    if (!meta.explicitSenders) patch.email_tags = d.emailTagIds;
    await updateCampaign(id, patch);
    if (!meta.stepsLocked) {
        // Fresh, so a failed save's leftovers are swept up and a branch added meanwhile is kept.
        const current = await getSequences(id);
        const known = new Set(meta.knownStepIds);
        if (isRewritable(current) && current.every((s) => known.has(s.id))) {
            await syncSteps(id, writtenEmails(d), current, (created) => meta.knownStepIds.push(created));
        }
    }
    if (!sameSet(d.segmentIds, meta.segmentIds)) await setCampaignSegments(id, d.segmentIds);
    return id;
}

// Writes the chain: new emails are created, changed ones patched, removed ones
// deleted, and every step pointed at the next before anything is deleted.
async function syncSteps(id: string, emails: EmailDraft[], before: Sequence[], onCreated: (id: string) => void) {
    const byId = new Map(before.map((s) => [s.id, s]));
    const ids: string[] = [];
    for (const e of emails) {
        if (e.serverId && byId.has(e.serverId)) {
            ids.push(e.serverId);
            continue;
        }
        const created = (await createSequence(id)).id;
        onCreated(created);
        ids.push(created);
    }
    for (let i = 0; i < emails.length; i++) {
        const e = emails[i];
        const prev = byId.get(ids[i]);
        const next = ids[i + 1] ?? null;
        const branch = prev?.conditions?.branches?.[0];
        const content = {
            // A name given on the Steps tab stays; an automatic one follows the position.
            name: prev?.name && !/^Step \d+$/.test(prev.name) ? prev.name : `Step ${i + 1}`,
            subject: e.subject.trim(),
            body_html: e.body_html,
            body_plain: e.body_plain || htmlToPlain(e.body_html),
            body_code: e.body_code,
            wait_after: i === 0 ? 0 : Math.max(0, e.wait_after),
        };
        const conditions = next
            ? { branches: [{ branch_id: branch?.branch_id ?? crypto.randomUUID(), target_step_id: next, conditions: [] }] }
            : { branches: [] };
        const contentChanged =
            !prev ||
            prev.name !== content.name ||
            prev.subject !== content.subject ||
            prev.body_html !== content.body_html ||
            prev.body_plain !== content.body_plain ||
            prev.body_code !== content.body_code ||
            prev.wait_after !== content.wait_after;
        const linkChanged = (prev?.conditions?.branches?.[0]?.target_step_id ?? null) !== next || (prev?.conditions?.branches?.length ?? 0) !== (next ? 1 : 0);
        if (contentChanged || linkChanged) {
            await updateSequence(id, ids[i], { ...(contentChanged ? content : {}), ...(linkChanged ? { conditions } : {}) });
        }
    }
    for (const s of before) {
        if (!ids.includes(s.id)) await deleteSequence(id, s.id);
    }
}

// What the save compares against to decide whether closing needs a write.
export function draftSignature(d: Draft): string {
    return JSON.stringify({ ...d, emails: d.emails.filter(hasContent).map(({ id: _id, ...rest }) => rest) });
}
