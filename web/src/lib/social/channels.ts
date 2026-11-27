// Labels for social channels, actions and consent bases. What each channel can do (its actions, sign-in methods,
// limits, consent bases, reactions) comes from GET /social/channels; nothing here decides it.
import type { ChannelSpec, ConsentBasis, SocialActionType, SocialChannel } from "@/lib/api/models/app/social/Social";

/** Display order. */
export const CHANNELS: SocialChannel[] = ["linkedin", "instagram", "whatsapp", "telegram", "facebook"];

export const CHANNEL_LABEL: Record<SocialChannel, string> = {
    linkedin: "LinkedIn",
    instagram: "Instagram",
    whatsapp: "WhatsApp",
    telegram: "Telegram",
    facebook: "Facebook",
};

export const isChannel = (v: unknown): v is SocialChannel => typeof v === "string" && (CHANNELS as string[]).includes(v);

export function channelLabel(c: string | null | undefined, catalog?: ChannelSpec[]): string {
    if (!c) return "";
    return catalog?.find((s) => s.channel === c)?.label ?? (isChannel(c) ? CHANNEL_LABEL[c] : c);
}

/** "LinkedIn, Instagram and Facebook" */
export function channelList(channels: readonly string[], conjunction: "and" | "or" = "and"): string {
    const names = channels.map((c) => channelLabel(c));
    return names.length > 1 ? `${names.slice(0, -1).join(", ")} ${conjunction} ${names[names.length - 1]}` : (names[0] ?? "");
}

/** Catalog entries in display order. */
export function orderedCatalog(catalog: ChannelSpec[] | undefined): ChannelSpec[] {
    return [...(catalog ?? [])].sort((a, b) => CHANNELS.indexOf(a.channel) - CHANNELS.indexOf(b.channel));
}

export interface ActionMeta {
    label: string;
    /** One line for a picker or a summary. */
    hint: string;
    description: string;
    /** Added to the description only on LinkedIn. */
    linkedin?: string;
    keywords: string;
}

export const ACTION_META: Record<SocialActionType, ActionMeta> = {
    invite: {
        label: "Connection request",
        hint: "With an optional note",
        description: "Ask to connect, with an optional note. Branch on whether they accept.",
        keywords: "invite connect connection request invitation note",
    },
    message: {
        label: "Message",
        hint: "A direct message",
        description: "Send a direct message.",
        linkedin: "On LinkedIn it reaches 1st-degree connections only.",
        keywords: "message dm chat direct follow up",
    },
    inmail: {
        label: "InMail",
        hint: "Sales Navigator, Recruiter",
        description: "Message someone outside the account's network. Needs Premium, Sales Navigator or Recruiter, and uses an InMail credit unless they have an Open Profile.",
        keywords: "inmail premium sales navigator recruiter",
    },
    voice_note: {
        label: "Voice note",
        hint: "A recording, or a written script",
        description: "Send a voice message: one recording for everyone, or a script each contact hears in the account's own voice.",
        keywords: "voice note audio recording speech",
    },
    profile_visit: {
        label: "Profile visit",
        hint: "They may see the visit",
        description: "View the contact's profile. Best a day or two before an invite.",
        keywords: "profile visit view",
    },
    follow: {
        label: "Follow",
        hint: "Without connecting",
        description: "Follow the contact, so they get a notification and the account sees their posts.",
        keywords: "follow",
    },
    like_post: {
        label: "Like a post",
        hint: "Their latest post",
        description: "React to the contact's most recent post. A contact with no recent post is skipped.",
        keywords: "like react reaction post engage",
    },
    comment_post: {
        label: "Comment on a post",
        hint: "From comments you approve",
        description: "Comment on the contact's latest post with one of the comments you wrote. A contact with no recent post is skipped.",
        keywords: "comment reply post engage",
    },
    endorse: {
        label: "Endorse skills",
        hint: "Connections only",
        description: "Endorse up to 3 of the contact's skills. Only 1st-degree connections can be endorsed; others are skipped.",
        keywords: "endorse skills endorsement",
    },
    withdraw_invite: {
        label: "Withdraw invite",
        hint: "An unanswered invite",
        description: "Withdraw a connection request this campaign sent that is still waiting. Anyone else skips this step.",
        keywords: "withdraw cancel invite invitation",
    },
};

/** Picker order of actions. */
export const ACTION_ORDER: SocialActionType[] = ["invite", "message", "inmail", "voice_note", "profile_visit", "follow", "like_post", "comment_post", "endorse", "withdraw_invite"];

export function actionLabel(a: string | null | undefined): string {
    if (!a) return "";
    return (ACTION_META as Record<string, ActionMeta>)[a]?.label ?? a.replace(/_/g, " ").replace(/^./, (c) => c.toUpperCase());
}

/** The channels whose catalog offers an action. */
export function channelsWith(catalog: ChannelSpec[] | undefined, action: string): SocialChannel[] {
    return orderedCatalog(catalog)
        .filter((c) => c.actions.includes(action))
        .map((c) => c.channel);
}

/** The usage meters an account row shows: the channel's sending actions, at most four. */
export function rowActions(actions: string[]): string[] {
    const order = ["invite", "message", "inmail", "voice_note", "profile_visit", "follow", "like_post", "comment_post"];
    return order.filter((a) => actions.includes(a)).slice(0, 4);
}

export const CONSENT_LABEL: Record<ConsentBasis, { label: string; hint: string }> = {
    opt_in: { label: "Opted in", hint: "They asked to hear from you on this channel." },
    existing_conversation: { label: "Existing conversation", hint: "You already talk with them here." },
    imported_with_consent: { label: "Imported with consent", hint: "Your import records that they agreed." },
    friend: { label: "Friend", hint: "They are a friend of the sending profile." },
};

export function consentLabel(b: string | null | undefined): string {
    if (!b) return "";
    return (CONSENT_LABEL as Record<string, { label: string }>)[b]?.label ?? b.replace(/_/g, " ").replace(/^./, (c) => c.toUpperCase());
}

export const AUTH_METHOD_LABEL: Record<string, string> = {
    password: "Username and password",
    cookie: "Session cookie",
    qr: "QR code",
    pairing_code: "Pairing code",
};

/** What a contact's handle looks like on each channel, for placeholders. */
export const HANDLE_PLACEHOLDER: Record<SocialChannel, string> = {
    linkedin: "linkedin.com/in/jane-doe",
    instagram: "@janedoe",
    whatsapp: "+447700900123",
    telegram: "@janedoe",
    facebook: "facebook.com/profile.php?id=100012345678901",
};

/** The forms each channel accepts, in a sentence for hints and refusals. */
export const HANDLE_HINT: Record<SocialChannel, string> = {
    linkedin: "A LinkedIn profile address or its public id.",
    instagram: "An Instagram username, profile address or numeric id.",
    whatsapp: "A phone number in international format, starting with + and the country code.",
    telegram: "A Telegram @username, t.me link or numeric id. Phone numbers are not accepted.",
    facebook: "A Facebook numeric id, or a profile address with it (profile.php?id= or /people/…/id). Vanity names are not accepted.",
};

function parseUrl(raw: string, hosts: string[]): URL | null {
    try {
        const u = new URL(/^https?:\/\//i.test(raw) ? raw : `https://${raw}`);
        const host = u.hostname.toLowerCase();
        return hosts.some((x) => host === x || host.endsWith(`.${x}`)) ? u : null;
    } catch {
        return null;
    }
}

const looksLikeUrl = (h: string) => /^https?:\/\//i.test(h) || /^[a-z0-9.-]+\.[a-z]{2,}\//i.test(h);
const pathParts = (u: URL) => u.pathname.split("/").filter(Boolean).map((p) => decodeURIComponent(p));

/** The canonical parts of a handle, or null when it does not fit the channel. */
function parseHandle(channel: SocialChannel, raw: string | null | undefined): { kind: "id" | "username" | "phone"; value: string } | null {
    const h = (raw ?? "").trim();
    if (!h) return null;
    switch (channel) {
        case "linkedin": {
            if (looksLikeUrl(h)) {
                const u = parseUrl(h, ["linkedin.com"]);
                const parts = u ? pathParts(u) : [];
                return parts[0] === "in" && parts[1] && /^[\p{L}\p{N}_%.-]{2,100}$/u.test(parts[1]) ? { kind: "username", value: parts[1] } : null;
            }
            return /^[\p{L}\p{N}_%.-]{2,100}$/u.test(h) ? { kind: "username", value: h } : null;
        }
        case "instagram": {
            if (looksLikeUrl(h)) {
                const u = parseUrl(h, ["instagram.com"]);
                const parts = u ? pathParts(u) : [];
                return parts.length === 1 && /^[A-Za-z0-9._]{1,30}$/.test(parts[0]) ? { kind: "username", value: parts[0].toLowerCase() } : null;
            }
            const bare = h.replace(/^@/, "");
            if (/^\d{5,20}$/.test(bare) && !h.startsWith("@")) return { kind: "id", value: bare };
            return /^[A-Za-z0-9._]{1,30}$/.test(bare) ? { kind: "username", value: bare.toLowerCase() } : null;
        }
        case "whatsapp": {
            if (!h.startsWith("+")) return null;
            const digits = h.slice(1).replace(/[\s().-]/g, "");
            return /^[1-9]\d{6,14}$/.test(digits) ? { kind: "phone", value: `+${digits}` } : null;
        }
        case "telegram": {
            if (h.startsWith("+")) return null;
            if (looksLikeUrl(h)) {
                const u = parseUrl(h, ["t.me", "telegram.me"]);
                const parts = u ? pathParts(u) : [];
                return parts.length === 1 && /^[A-Za-z][A-Za-z0-9_]{3,31}$/.test(parts[0]) ? { kind: "username", value: parts[0].toLowerCase() } : null;
            }
            if (/^\d{5,20}$/.test(h)) return { kind: "id", value: h };
            const bare = h.replace(/^@/, "");
            return /^[A-Za-z][A-Za-z0-9_]{3,31}$/.test(bare) ? { kind: "username", value: bare.toLowerCase() } : null;
        }
        case "facebook": {
            if (/^\d{5,20}$/.test(h)) return { kind: "id", value: h };
            if (!looksLikeUrl(h)) return null;
            const u = parseUrl(h, ["facebook.com", "fb.com"]);
            if (!u) return null;
            const parts = pathParts(u);
            if (parts[0] === "profile.php") {
                const id = u.searchParams.get("id") ?? "";
                return /^\d{5,20}$/.test(id) ? { kind: "id", value: id } : null;
            }
            const last = parts[parts.length - 1] ?? "";
            return parts[0] === "people" && parts.length >= 3 && /^\d{5,20}$/.test(last) ? { kind: "id", value: last } : null;
        }
    }
}

/** Why a handle does not fit its channel, or null when it does. Mirrors the server's check so a form can say so first. */
export function handleProblem(channel: SocialChannel, raw: string | null | undefined): string | null {
    if (!(raw ?? "").trim()) return null;
    return parseHandle(channel, raw) ? null : `That does not look like a ${CHANNEL_LABEL[channel]} handle. ${HANDLE_HINT[channel]}`;
}

/** A link to a contact's profile from their handle, or null when the handle does not name one. */
export function profileUrl(channel: SocialChannel, handle: string | null | undefined): string | null {
    const p = parseHandle(channel, handle);
    if (!p) return null;
    switch (channel) {
        case "linkedin":
            return `https://www.linkedin.com/in/${encodeURIComponent(p.value)}`;
        case "instagram":
            return p.kind === "username" ? `https://www.instagram.com/${p.value}` : null;
        case "whatsapp":
            return `https://wa.me/${p.value.slice(1)}`;
        case "telegram":
            return p.kind === "username" ? `https://t.me/${p.value}` : null;
        case "facebook":
            return `https://www.facebook.com/profile.php?id=${p.value}`;
    }
}
