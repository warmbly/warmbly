// State, validation and persistence for the new-campaign flow. Kept out of
// the dialog so the steps, the review and the launch plan read one model.

import type { CreateCampaignInput } from "@/lib/api/client/app/campaigns/createCampaign";
import { htmlToPlain } from "@/components/app/campaigns/sequences/emailPreview";
import { rampUpInvalid, rampUpPayload, type RampUp } from "@/components/app/campaigns/rampUp";

export type EmailDraft = {
    id: string;
    // The saved step this email edits, once the campaign exists.
    serverId?: string;
    subject: string;
    body_html: string;
    body_plain: string;
    body_code: boolean;
    // Days after the previous email; ignored on the first.
    wait_after: number;
};

export type StartMode = "now" | "later";

export type Draft = {
    name: string;
    // Once the user types a name it stops following the lead lists.
    nameTouched: boolean;
    description: string;
    // Lead lists (segments). Empty is fine: leads can be added after creating it.
    segmentIds: string[];
    emails: EmailDraft[];
    timezone: string;
    // Read the window in each lead's own timezone instead of the one above.
    recipientTime: boolean;
    days: number;
    startTime: string;
    endTime: string;
    startMode: StartMode;
    // Local "yyyy-MM-ddTHH:mm", the DateTimePicker's shape.
    scheduledAt: string;
    emailTagIds: string[];
    dailyLimit: number;
    rampUp: RampUp;
    stopOnReply: boolean;
    stopOnMeeting: boolean;
    openTracking: boolean;
    linkTracking: boolean;
    utmTracking: boolean;
    unsubHeader: boolean;
};

export type StepKey = "leads" | "emails" | "schedule" | "review";

export const STEPS: readonly { key: StepKey; label: string }[] = [
    { key: "leads", label: "Leads" },
    { key: "emails", label: "Emails" },
    { key: "schedule", label: "Schedule" },
    { key: "review", label: "Review" },
];

export const WEEKDAYS = ["Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"];
export const WEEKDAYS_MASK = 0b0011111;
export const EVERY_DAY_MASK = 0b1111111;
export const NAME_MIN = 3;
export const NAME_MAX = 50;
export const WAIT_MAX = 60;

let emailCounter = 0;
export const newEmail = (wait: number): EmailDraft => ({
    id: `email-${Date.now().toString(36)}-${++emailCounter}`,
    subject: "",
    body_html: "",
    body_plain: "",
    body_code: false,
    wait_after: wait,
});

export const initialDraft = (timezone: string): Draft => ({
    name: "",
    nameTouched: false,
    description: "",
    segmentIds: [],
    emails: [newEmail(0)],
    timezone,
    recipientTime: false,
    days: WEEKDAYS_MASK,
    startTime: "08:00",
    endTime: "18:00",
    startMode: "now",
    scheduledAt: "",
    emailTagIds: [],
    dailyLimit: 50,
    rampUp: { warmup_graduation: true, ramp_enabled: false, ramp_start: 10, ramp_increment: 5, ramp_ceiling: 50 },
    stopOnReply: true,
    stopOnMeeting: true,
    openTracking: true,
    linkTracking: true,
    utmTracking: true,
    unsubHeader: true,
});

export function hasBody(e: EmailDraft): boolean {
    return (e.body_plain || htmlToPlain(e.body_html)).trim().length > 0;
}

export function hasContent(e: EmailDraft): boolean {
    return e.subject.trim().length > 0 || hasBody(e);
}

// The emails that will be created: blank follow-ups are dropped.
export function writtenEmails(d: Draft): EmailDraft[] {
    return d.emails.filter(hasContent);
}

export function stepWaits(d: Draft): number[] {
    return writtenEmails(d)
        .slice(1)
        .map((e) => Math.max(0, e.wait_after));
}

export function scheduledDate(d: Draft): Date | null {
    if (d.startMode !== "later" || !d.scheduledAt) return null;
    const date = new Date(d.scheduledAt);
    return Number.isNaN(date.getTime()) ? null : date;
}

// The name a draft gets until the user writes one.
export function autoName(segmentNames: string[], now = new Date()): string {
    const base = segmentNames[0]?.trim();
    if (base) {
        const extra = segmentNames.length > 1 ? ` +${segmentNames.length - 1}` : "";
        const name = `${base}${extra} outreach`;
        if (name.length <= NAME_MAX) return name;
    }
    return `Campaign, ${now.toLocaleDateString("en-US", { month: "short", day: "numeric" })}`;
}

export function emailIssue(e: EmailDraft, index: number): string | null {
    const label = index === 0 ? "The first email" : `Follow-up ${index}`;
    const subject = e.subject.trim().length > 0;
    const body = hasBody(e);
    if (index === 0) {
        if (body && !subject) return `${label} needs a subject line`;
        if (subject && !body) return `${label} needs a body`;
        return null;
    }
    // A follow-up with no subject replies in the thread; with no body it is empty.
    if (subject && !body) return `${label} needs a body`;
    return null;
}

// One human-readable reason a step cannot be left yet, or null when it can.
export function stepIssue(key: StepKey, d: Draft): string | null {
    switch (key) {
        case "leads": {
            const n = d.name.trim().length;
            if (n > 0 && n < NAME_MIN) return `The name needs at least ${NAME_MIN} characters`;
            if (n > NAME_MAX) return `The name is ${NAME_MAX} characters max`;
            return null;
        }
        case "emails": {
            for (let i = 0; i < d.emails.length; i++) {
                const issue = emailIssue(d.emails[i], i);
                if (issue) return issue;
            }
            if (!hasContent(d.emails[0]) && d.emails.slice(1).some(hasContent)) {
                return "Write the first email before its follow-ups";
            }
            return null;
        }
        case "schedule": {
            if (d.days === 0) return "Pick at least one sending day";
            if (d.startTime && d.endTime && d.startTime >= d.endTime) return "End time must be after the start time";
            if (d.startMode === "later") {
                const at = scheduledDate(d);
                if (!at) return "Pick a date and time to start";
                if (at.getTime() < Date.now()) return "The start time has already passed";
            }
            if (d.dailyLimit < 3 || d.dailyLimit > 5000) return "Daily limit must be between 3 and 5,000";
            const ramp = rampUpInvalid(d.rampUp);
            if (ramp) return `Ramp-up: ${ramp.charAt(0).toLowerCase()}${ramp.slice(1)}`;
            return null;
        }
        default:
            return null;
    }
}

export function firstIssue(d: Draft): { key: StepKey; issue: string } | null {
    for (const s of STEPS) {
        const issue = stepIssue(s.key, d);
        if (issue) return { key: s.key, issue };
    }
    return null;
}

export function isDirty(d: Draft): boolean {
    return (
        d.nameTouched ||
        d.description.trim() !== "" ||
        d.segmentIds.length > 0 ||
        d.emailTagIds.length > 0 ||
        d.emails.some(hasContent) ||
        d.emails.length > 1
    );
}

// The wizard's answers as a create call. The server derives thread_reply from
// the subjects, so it is deliberately not sent.
export function toCreateInput(d: Draft, name: string): CreateCampaignInput {
    const at = scheduledDate(d);
    return {
        name,
        description: d.description.trim(),
        stop_on_reply: d.stopOnReply,
        stop_on_meeting: d.stopOnMeeting,
        timezone: d.timezone,
        timezone_mode: d.recipientTime ? "recipient" : "campaign",
        days: d.days,
        start_time: d.startTime,
        end_time: d.endTime,
        daily_limit: d.dailyLimit,
        ...rampUpPayload(d.rampUp),
        open_tracking: d.openTracking,
        link_tracking: d.linkTracking,
        utm_tracking: d.utmTracking,
        unsubscribe_header: d.unsubHeader,
        email_tag_ids: d.emailTagIds,
        start_date: at ? at.toISOString() : undefined,
        steps: writtenEmails(d).map((e, i) => ({
            name: `Step ${i + 1}`,
            subject: e.subject.trim(),
            body_html: e.body_html,
            body_plain: e.body_plain || htmlToPlain(e.body_html),
            body_code: e.body_code,
            wait_after: i === 0 ? 0 : Math.max(0, e.wait_after),
        })),
    };
}

export function daysLabel(mask: number): string {
    if (mask === EVERY_DAY_MASK) return "Every day";
    if (mask === WEEKDAYS_MASK) return "Weekdays";
    const on = WEEKDAYS.filter((_, i) => (mask & (1 << i)) !== 0).map((d) => d.slice(0, 3));
    return on.length === 0 ? "No days" : on.join(", ");
}

// "14:30" -> "2:30 PM"
export function fmt12(hhmm: string): string {
    const [h, m] = hhmm.split(":").map(Number);
    if (Number.isNaN(h) || Number.isNaN(m)) return hhmm;
    const h12 = h % 12 === 0 ? 12 : h % 12;
    return `${h12}:${String(m).padStart(2, "0")} ${h < 12 ? "AM" : "PM"}`;
}

// A projection day is midnight in the campaign's zone, so it is read there,
// not in the browser's, or a western browser shows the day before.
export function fmtDay(d: Date | string | null | undefined, timeZone?: string): string {
    if (!d) return "";
    const date = typeof d === "string" ? new Date(`${d}T12:00:00Z`) : d;
    if (Number.isNaN(date.getTime())) return "";
    const opts: Intl.DateTimeFormatOptions = { month: "short", day: "numeric" };
    if (typeof d !== "string" && timeZone) {
        try {
            return date.toLocaleDateString("en-US", { ...opts, timeZone });
        } catch {
            // An unknown zone falls through to UTC for strings, local for dates.
        }
    }
    if (typeof d === "string") return date.toLocaleDateString("en-US", { ...opts, timeZone: "UTC" });
    return date.toLocaleDateString("en-US", opts);
}

export function fmtDateTime(d: Date): string {
    return d.toLocaleString("en-US", { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" });
}

export function plural(n: number, one: string, many = `${one}s`): string {
    return `${n.toLocaleString()} ${n === 1 ? one : many}`;
}

// The stop rules that are on, joined for the sentence they end ("replied or booked a meeting").
export function stopRulePhrase(d: Draft, reply: string, meeting: string): string {
    return [d.stopOnReply && reply, d.stopOnMeeting && meeting].filter(Boolean).join(" or ");
}

export function trackingSummary(d: Draft): string {
    const on = [
        d.stopOnReply && "stop on reply",
        d.stopOnMeeting && "stop on meeting",
        d.openTracking && "opens",
        d.linkTracking && "clicks",
        d.utmTracking && "UTM",
        d.unsubHeader && "unsubscribe header",
    ].filter(Boolean) as string[];
    return on.length === 0 ? "All off" : on.join(", ").replace(/^./, (c) => c.toUpperCase());
}
