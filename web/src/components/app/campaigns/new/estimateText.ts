// The sentences a campaign estimate is read through: when it finishes and what
// holds it back.

import type { CampaignEstimateResult, CampaignEstimateScenario } from "@/lib/api/client/app/campaigns/estimateCampaign";
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
    const pace = paceSentence(e, days);
    if (steps <= 1) {
        return {
            title: `Everyone gets it by ${fmtDay(finish, tz)}`,
            detail: `${pace}.`,
            tone,
        };
    }
    return {
        title: `Finishes around ${fmtDay(finish, tz)}`,
        detail: firstTouch
            ? `${pace}. Everyone has the first email by ${fmtDay(firstTouch, tz)}, and follow-ups run to ${fmtDay(finish, tz)}. Counted as if nobody replies.`
            : `${pace}. Counted as if nobody replies.`,
        tone,
    };
}

// The arithmetic behind the finish date: how much, how fast, how many days.
function paceSentence(e: CampaignEstimateResult, days: number): string {
    const perDay = Math.round(e.total_sends / Math.max(1, days));
    // Day 0 can be cut short by the start time, so the week is read from day 1.
    const week = e.timeline.slice(1, 8);
    const perWeek = week.filter((d) => d.sending_day).length;
    const weekly = week.length === 7 && perWeek < 7 ? `, ${perWeek} ${perWeek === 1 ? "day" : "days"} a week` : "";
    return `${plural(e.total_sends, "email")} at about ${perDay.toLocaleString()} a day is ${plural(Math.max(1, days), "sending day")}${weekly}`;
}

// What sets the pace, in a few words for beside the finish date.
export function paceLabel(e: CampaignEstimateResult | undefined): string | null {
    switch (e?.bottleneck) {
        case "warmup_graduation":
            return "Paced by replies";
        case "spacing":
            return "Paced by the sending window";
        case "campaign_limit":
            return "Paced by the daily limit";
        case "campaign_ramp":
            return "Paced by the ramp-up";
        case "other_campaigns":
            return "Shared with other campaigns";
        case "health":
            return "Paced by mailbox health";
        case "held":
            return "Mailboxes on hold";
        case "workspace_risk":
            return "Paced by workspace risk";
        case "org_daily_limit":
            return "Paced by your plan";
        case "sending_behavior":
            return "Paced by sending behaviour";
        default:
            return null;
    }
}

// A change the estimate simulated, in words.
export function scenarioText(s: CampaignEstimateScenario): { title: string; detail: string } {
    switch (s.kind) {
        case "replies":
            return {
                title: `If ${s.value}% of leads reply`,
                detail: "Each reply lets its mailbox send one more a day. A common reply rate for cold outreach.",
            };
        case "more_mailboxes":
            return {
                title: `With ${plural(s.value, "more mailbox", "more mailboxes")}`,
                detail: "Twice the senders, each starting at the same pace as these.",
            };
        case "every_day":
            return { title: "Sending every day", detail: "Weekends too, in the same hours." };
    }
}

// What would not move the date, so nobody reaches for it.
export function wontHelpText(e: CampaignEstimateResult): string | null {
    switch (e.bottleneck) {
        case "warmup_graduation":
            return "Raising the daily limit would not change the date: replies set the pace, not the limit.";
        case "spacing":
            return "Raising the daily limit would not change the date: mailboxes run out of hours first.";
        default:
            return null;
    }
}

// The campaign emails a ramping mailbox starts at each day, read off the pool.
// A ramping sender can carry another state (reconnecting, say), so with none
// labelled ramping the smallest first-day cap stands in.
export function rampStart(e: CampaignEstimateResult): number | null {
    if (e.ramping === 0) return null;
    const ramping = e.senders.filter((s) => s.state === "ramping").map((s) => s.first_day_cap);
    if (ramping.length > 0) return Math.max(...ramping);
    const caps = e.senders.map((s) => s.first_day_cap).filter((n) => n > 0);
    return caps.length > 0 ? Math.min(...caps) : null;
}

// Why it takes as long as it does, in the pool's own terms. limit is the
// campaign's maximum per mailbox per day.
export function bottleneckText(e: CampaignEstimateResult, limit?: number): string | null {
    switch (e.bottleneck) {
        case "warmup_graduation": {
            const start = rampStart(e);
            const begin = start !== null ? `start at ${plural(start, "campaign email")} a day` : "start slowly";
            const upTo = limit ? `, up to ${limit}` : "";
            return `${plural(e.ramping, "mailbox", "mailboxes")} earn campaign volume from replies, so they ${begin}. Each lead who replied in the last 7 days lets the mailbox they wrote to send one more a day${upTo}; a bounce pauses that climb and a spam complaint resets it. This estimate assumes nobody replies, so it is the slowest case. Warmup replies do not count.`;
        }
        case "spacing":
            return "The sending window is the limit: with the gap between sends, and warmup sharing the same clock, mailboxes run out of hours before they reach their cap. A wider window goes faster.";
        case "campaign_ramp":
            return "The ramp-up schedule is the limit: every mailbox starts at its first-day volume and adds the same amount each sending day until the ceiling. A higher start or a faster increase goes sooner.";
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
