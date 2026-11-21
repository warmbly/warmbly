// A social step's defaults, one-line summary and the problems the backend would refuse it for (models/social_step.go).
import type { SocialStepConfig, SocialVariant } from "@/lib/api/models/app/campaigns/sequences/Action";
import { SOCIAL_BASE_VARIANT, SOCIAL_INMAIL_SUBJECT_MAX, SOCIAL_MAX_COMMENTS, SOCIAL_MAX_POST_AGE_DAYS, SOCIAL_MAX_VARIANTS, SOCIAL_VOICE_SCRIPT_MAX } from "@/lib/api/models/app/campaigns/sequences/Action";
import type { ChannelSpec, SocialChannel } from "@/lib/api/models/app/social/Social";
import { ACTION_META, ACTION_ORDER, CHANNELS, actionLabel, channelLabel, channelsWith, isChannel } from "@/lib/social/channels";
import { renderPreview } from "./emailPreview";

/** What a contact sees once variables are filled in for the sample contact, which is what a channel's limit counts. */
export const renderedLength = (text: string | undefined) => renderPreview(text ?? "").trim().length;

/** The backend accepts an invite note as written up to twice the limit, since variables usually render shorter. */
const NOTE_SLACK = 2;

export const hasVariants = (action: string) => action === "invite" || action === "message" || action === "inmail";

export function defaultSocialConfig(channel: SocialChannel, action: string): SocialStepConfig {
    const base: SocialStepConfig = { channel, action, if_missing: "skip" };
    switch (action) {
        case "invite":
            return { ...base, note: "" };
        case "message":
            return { ...base, body: "" };
        case "inmail":
            return { ...base, subject: "", body: "" };
        case "voice_note":
            return { ...base, voice: { mode: "recording" } };
        case "like_post":
            return { ...base, reaction: channel === "linkedin" ? "like" : undefined, post_age_days: 30 };
        case "comment_post":
            return { ...base, comments: [""], post_age_days: 30 };
        case "endorse":
            return { ...base, max_skills: 1 };
        default:
            return base;
    }
}

/** Keeps what still applies when the channel or action changes: copy that both take, and if_missing. */
export function switchSocialConfig(prev: SocialStepConfig | undefined, channel: SocialChannel, action: string, spec?: ChannelSpec): SocialStepConfig {
    const next = defaultSocialConfig(channel, action);
    if (!prev) return next;
    next.if_missing = prev.if_missing ?? "skip";
    if (action === prev.action) {
        const kept = { ...prev, channel, if_missing: next.if_missing };
        if (kept.reaction && !(spec?.reactions ?? []).includes(kept.reaction)) kept.reaction = next.reaction;
        if (!hasVariants(action)) delete kept.variants;
        return kept;
    }
    if ((action === "message" || action === "inmail") && (prev.action === "message" || prev.action === "inmail")) {
        next.body = prev.body;
        next.fallback_body = prev.fallback_body;
    }
    return next;
}

const CHANNEL_WORDS: [RegExp, SocialChannel][] = [
    [/\b(linkedin|li|sales ?nav(igator)?|recruiter)\b/, "linkedin"],
    [/\b(instagram|insta|ig)\b/, "instagram"],
    [/\b(whatsapp|wa)\b/, "whatsapp"],
    [/\b(telegram|tg)\b/, "telegram"],
    [/\b(facebook|fb|messenger)\b/, "facebook"],
];

/** The channel and action a picker search names, e.g. "whatsapp voice" or "like post". */
export function socialPickFromQuery(query: string, catalog: ChannelSpec[] | undefined, preferred: SocialChannel[]): { channel: SocialChannel; action: string } {
    const q = query.toLowerCase();
    const named = CHANNEL_WORDS.find(([re]) => re.test(q))?.[1];
    const words = q.split(/\s+/).filter((w) => w.length > 1 && !CHANNEL_WORDS.some(([re]) => re.test(w)));
    const action = words.length
        ? ACTION_ORDER.find((a) => {
              const hay = `${a.replace(/_/g, " ")} ${ACTION_META[a].label} ${ACTION_META[a].keywords}`.toLowerCase();
              return words.every((w) => hay.includes(w));
          })
        : undefined;
    const offered = (c: SocialChannel, a: string) => !catalog || !!catalog.find((s) => s.channel === c)?.actions.includes(a);
    const firstAction = (c: SocialChannel) => catalog?.find((s) => s.channel === c)?.actions[0] ?? "message";
    if (named && action && offered(named, action)) return { channel: named, action };
    if (named) return { channel: named, action: offered(named, "message") ? "message" : firstAction(named) };
    if (action) {
        const can = channelsWith(catalog, action);
        const channel = preferred.find((c) => can.includes(c)) ?? can[0] ?? "linkedin";
        return { channel, action };
    }
    const channel = preferred[0] ?? (catalog?.[0]?.channel as SocialChannel | undefined) ?? "linkedin";
    return { channel, action: offered(channel, "message") ? "message" : firstAction(channel) };
}

const clip = (t: string, n = 64) => (t.length > n ? `${t.slice(0, n - 3)}…` : t);
const oneLine = (t?: string) => (t ?? "").replace(/\s+/g, " ").trim();

export function socialStepTitle(cfg: SocialStepConfig | undefined): string {
    if (!cfg) return "Social step";
    return `${actionLabel(cfg.action)} on ${channelLabel(cfg.channel)}`;
}

export function socialSummary(cfg: SocialStepConfig | undefined, spec?: ChannelSpec): string {
    if (!cfg) return "Pick a channel and an action…";
    const variants = cfg.variants?.length ? ` · ${cfg.variants.length + 1} versions` : "";
    switch (cfg.action) {
        case "invite": {
            const note = oneLine(cfg.note);
            if (!note) return "Invite without a note";
            const max = spec?.invite_note_max_chars ?? 0;
            return max && renderedLength(cfg.note) > max ? `Note is over ${max} characters` : clip(note) + variants;
        }
        case "message":
        case "inmail": {
            const body = oneLine(cfg.body);
            if (!body) return cfg.action === "inmail" ? "Write the InMail…" : "Write the message…";
            return clip(oneLine(cfg.subject) || body) + variants;
        }
        case "voice_note":
            if (cfg.voice?.mode === "written") return oneLine(cfg.voice.script) ? `Voice: ${clip(oneLine(cfg.voice.script), 56)}` : "Write the script…";
            return cfg.voice?.audio_id ? `Recording · ${cfg.voice.duration_seconds ?? 0}s` : "Upload a recording…";
        case "comment_post": {
            const n = (cfg.comments ?? []).filter((c) => c.trim()).length;
            return n ? `${n} ${n === 1 ? "comment" : "comments"} to pick from` : "Write a comment…";
        }
        case "like_post":
            return cfg.reaction && cfg.reaction !== "like" ? `React “${cfg.reaction}” to their latest post` : "Like their latest post";
        case "endorse":
            return `Endorse up to ${cfg.max_skills ?? 1} ${cfg.max_skills === 1 || !cfg.max_skills ? "skill" : "skills"}`;
        default:
            return ACTION_META[cfg.action as keyof typeof ACTION_META]?.description.split(".")[0] ?? actionLabel(cfg.action);
    }
}

export interface SocialIssue {
    field: string;
    message: string;
}

/** The text and age limits a step is held to, from the catalog, with the shared defaults for an older backend. */
export function socialLimits(spec: ChannelSpec | undefined, action?: string) {
    const message = spec?.message_max_chars ?? 0;
    return {
        note: spec?.invite_note_max_chars ?? 0,
        body: action === "inmail" ? (spec?.inmail_body_max_chars ?? message) : message,
        subject: spec?.inmail_subject_max_chars ?? SOCIAL_INMAIL_SUBJECT_MAX,
        comment: spec?.comment_max_chars ?? 0,
        script: spec?.voice_script_max_chars ?? SOCIAL_VOICE_SCRIPT_MAX,
        postAge: spec?.max_post_age_days ?? SOCIAL_MAX_POST_AGE_DAYS,
    };
}

/** Everything wrong with a step, worded for the person fixing it. The first is what the canvas shows. */
export function socialIssues(cfg: SocialStepConfig | undefined, spec: ChannelSpec | undefined): SocialIssue[] {
    if (!cfg) return [{ field: "action", message: "Pick a channel and an action." }];
    const out: SocialIssue[] = [];
    const add = (field: string, message: string) => out.push({ field, message });
    if (!isChannel(cfg.channel)) add("channel", "Pick a channel.");
    const name = channelLabel(cfg.channel);
    if (spec && !spec.actions.includes(cfg.action)) add("action", `${name} does not offer ${actionLabel(cfg.action).toLowerCase()}. Pick another action.`);
    if (cfg.if_missing && cfg.if_missing !== "skip" && cfg.if_missing !== "stop") add("if_missing", "Choose what happens to a contact with no handle.");

    const lim = socialLimits(spec, cfg.action);
    const noteMax = lim.note;
    const msgMax = lim.body;
    const commentMax = lim.comment;
    const checkNote = (field: string, t: string | undefined) => {
        if (!noteMax || !t) return;
        if ((t ?? "").length > noteMax * NOTE_SLACK || renderedLength(t) > noteMax) add(field, `Shorten the note to ${noteMax} characters.`);
    };
    const checkBody = (field: string, t: string | undefined) => {
        if (msgMax && t && ((t ?? "").length > msgMax || renderedLength(t) > msgMax)) add(field, `Shorten the message to ${msgMax} characters, ${name}'s limit.`);
    };
    const checkSubject = (field: string, t: string | undefined) => {
        if (t && t.length > lim.subject) add(field, `Shorten the subject to ${lim.subject} characters.`);
    };

    switch (cfg.action) {
        case "invite":
            checkNote("note", cfg.note);
            checkNote("fallback_note", cfg.fallback_note);
            break;
        case "message":
            if (!cfg.body?.trim()) add("body", "Write the message.");
            checkBody("body", cfg.body);
            checkBody("fallback_body", cfg.fallback_body);
            break;
        case "inmail":
            if (!cfg.body?.trim()) add("body", "Write the InMail.");
            checkSubject("subject", cfg.subject);
            checkSubject("fallback_subject", cfg.fallback_subject);
            checkBody("body", cfg.body);
            checkBody("fallback_body", cfg.fallback_body);
            break;
        case "comment_post": {
            const comments = cfg.comments ?? [];
            if (!comments.some((c) => c.trim())) add("comments", "Write at least one comment.");
            if (comments.length > SOCIAL_MAX_COMMENTS) add("comments", `A comment step takes at most ${SOCIAL_MAX_COMMENTS} comments.`);
            comments.forEach((c, i) => {
                if (commentMax && c.length > commentMax) add(`comments.${i}`, `Shorten this comment to ${commentMax} characters, ${name}'s limit.`);
            });
            break;
        }
        case "voice_note": {
            const v = cfg.voice;
            if (!v) add("voice", "Add a recording or a script.");
            else if (v.mode === "recording" && !v.audio_id) add("voice", "Upload a recording.");
            else if (v.mode === "written" && !v.script?.trim()) add("voice", "Write the script.");
            if (v && spec && (v.duration_seconds ?? 0) > spec.voice_note_max_seconds) add("voice", `A ${name} voice note is at most ${spec.voice_note_max_seconds} seconds.`);
            if (v?.script && v.script.length > lim.script) add("voice.script", `Shorten the script to ${lim.script} characters.`);
            if (v?.fallback_script && v.fallback_script.length > lim.script) add("voice.fallback_script", `Shorten the fallback script to ${lim.script} characters.`);
            break;
        }
        case "endorse":
            if (cfg.max_skills !== undefined && (cfg.max_skills < 1 || cfg.max_skills > 3)) add("max_skills", "Endorse between 1 and 3 skills.");
            break;
    }
    if (cfg.reaction && (cfg.action !== "like_post" || (spec && !spec.reactions.includes(cfg.reaction)))) add("reaction", `${name} does not offer that reaction.`);
    const age = cfg.post_age_days ?? 0;
    if (age < 0 || age > lim.postAge) add("post_age_days", `Post age is between 0 and ${lim.postAge} days.`);

    const variants = cfg.variants ?? [];
    if (variants.length && !hasVariants(cfg.action)) add("variants", "Only invites, messages and InMails take versions.");
    if (variants.length > SOCIAL_MAX_VARIANTS) add("variants", `A step takes at most ${SOCIAL_MAX_VARIANTS} extra versions.`);
    const seen = new Set([SOCIAL_BASE_VARIANT]);
    variants.forEach((v: SocialVariant, i) => {
        const id = v.id.trim();
        if (!id || id.length > 16 || seen.has(id)) add(`variants.${i}`, "Each version needs its own name.");
        seen.add(id);
        if (v.weight < 0 || v.weight > 100) add(`variants.${i}.weight`, "A version's share is between 0 and 100%.");
        if (cfg.action === "invite") checkNote(`variants.${i}.note`, v.note);
        if (cfg.action === "message" || cfg.action === "inmail") checkBody(`variants.${i}.body`, v.body);
        if (cfg.action === "inmail") checkSubject(`variants.${i}.subject`, v.subject);
    });
    if (variants.reduce((n, v) => n + (v.weight || 0), 0) > 100) add("variants", "The versions' shares add up to more than 100%.");
    if (cfg.winner && !seen.has(cfg.winner)) add("winner", "The winner names no version.");
    return out;
}

/** Channels in display order: those with an account first. */
export function preferredChannels(accountChannels: SocialChannel[]): SocialChannel[] {
    return [...CHANNELS].sort((a, b) => Number(accountChannels.includes(b)) - Number(accountChannels.includes(a)));
}
