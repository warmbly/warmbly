// The sentences a campaign estimate is read through: when it finishes and what
// holds it back.

import type { CampaignEstimateResult } from "@/lib/api/client/app/campaigns/estimateCampaign";
import { fmtDay, plural } from "./draft";

export type Headline = { title: string; detail: string; tone: "neutral" | "warn" };

// The one sentence the estimate is about.
export function estimateHeadline(
    e: CampaignEstimateResult | undefined,
    opts: { steps: number; hasLeads: boolean; tz?: string },
): Headline | null {
    if (!e) return null;
    const { steps, hasLeads, tz } = opts;
    if (e.mailboxes === 0) {
        return { title: "No mailbox can send this", detail: "The sender pool resolves to no active mailbox.", tone: "warn" };
    }
    if (e.steady_capacity === 0 && e.daily_capacity === 0) {
        return { title: "The pool has no sending capacity", detail: "Every mailbox in it is held right now.", tone: "warn" };
    }
    if (!hasLeads || e.recipients === 0) {
        return {
            title: `Up to ${e.steady_capacity.toLocaleString()} emails per sending day`,
            detail: hasLeads
                ? "The chosen lists are empty right now, so there is nothing to time."
                : "Add leads and this turns into a finish date.",
            tone: hasLeads ? "warn" : "neutral",
        };
    }
    const finish = e.estimated_finish_at;
    const firstTouch = e.first_touch_finish_at;
    if (!finish) {
        return {
            title: "Longer than two years at this pace",
            detail: `${plural(e.total_sends, "email")} at up to ${e.steady_capacity.toLocaleString()} a day. Add mailboxes, widen the window or narrow the leads.`,
            tone: "warn",
        };
    }
    const days = e.sending_days ?? 0;
    const tone = days > 21 ? "warn" : "neutral";
    if (steps <= 1) {
        return {
            title: `Everyone gets it by ${fmtDay(finish, tz)}`,
            detail: `${plural(e.recipients, "lead")} over ${plural(Math.max(1, days), "sending day")}.`,
            tone,
        };
    }
    return {
        title: `Finishes around ${fmtDay(finish, tz)}`,
        detail: firstTouch
            ? `Everyone has the first email by ${fmtDay(firstTouch, tz)}, then follow-ups run to the end. ${plural(e.total_sends, "email")} if nobody replies.`
            : `${plural(e.total_sends, "email")} if nobody replies.`,
        tone,
    };
}

// Why it takes as long as it does, in the pool's own terms.
export function bottleneckText(e: CampaignEstimateResult, _tz?: string): string | null {
    switch (e.bottleneck) {
        case "warmup_graduation":
            return `${plural(e.ramping, "mailbox", "mailboxes")} use conservative cold pacing. Synthetic test age does not unlock volume; recent real-recipient replies and negative feedback constrain increases. No full-speed date or reputation benefit is guaranteed.`;
        case "spacing":
            return "The sending window is the limit: with the gap between sends, and warmup sharing the same clock, mailboxes run out of hours before they reach their cap. A wider window goes faster.";
        case "campaign_limit":
            return "The configured daily limit per mailbox is the constraint. Raising it is not proof that a higher volume is safe; consider provider policies, recipient permission and observed feedback.";
        case "other_campaigns":
            return `These mailboxes already send about ${e.other_campaigns_per_day.toLocaleString()} a day for other campaigns, and that shares their daily caps.`;
        case "health":
            return "Some mailboxes are in a watch or throttled health band and send at reduced volume until they recover.";
        case "held":
            return `${plural(e.held, "mailbox", "mailboxes")} cannot send right now (a health hold, failing domain authentication, or resting), so the rest carry the campaign.`;
        case "workspace_risk":
            return "Your workspace's risk band is lowering the volume every mailbox may send.";
        case "org_daily_limit":
            return "Your plan's daily sending limit is the cap, not the mailboxes.";
        case "sending_behavior":
            return "Mailbox sending behaviour profiles (working days, daily and hourly ceilings) set the pace.";
        default:
            return null;
    }
}
