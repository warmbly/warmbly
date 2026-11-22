// Every social refusal in Warmbly's own words. Payment codes are the store's and fall through to its copy.
import type { AppError } from "@/lib/api/client/normalizeError";
import { storeErrorMessage } from "@/lib/buy/errors";

const COPY: Record<string, string> = {
    social_unavailable: "Social accounts are not available on this instance right now. Try again shortly.",
    social_mode_conflict: "This workspace already connects social accounts another way. Refresh to see how.",
    social_plan_required: "Social accounts need a Warmbly plan. Choose a plan, or use your own GraphKit workspace.",
    social_checkout_required: "Your GraphKit plan has no free seat. Add one in GraphKit, then try again.",
    social_sign_in_failed: "The sign-in was not accepted. Check the details and try again.",
    social_too_many_sign_ins: "Too many sign-in attempts. Wait a few minutes before trying again.",
    social_session_expired: "This sign-in timed out. Start again.",
    social_unpaid: "This account is paused because its month was not paid. Update your card and it resumes with the next payment.",
    social_invalid_request: "Some of the details were not accepted. Check them and try again.",
    social_acknowledgement_required: "Tick the box that you accept the risk of automating this account, then continue.",
    social_accept_required: "Connecting this account is charged. Accept the charge to continue.",
    invalid_social_handle: "That handle does not fit the channel. LinkedIn takes a profile address or id, Instagram a username, WhatsApp a phone number with +country code, Telegram an @username or numeric id, Facebook a numeric id or a profile address with it.",
    consent_required: "This channel needs a consent basis for the contact before anything is sent.",
    capability_unavailable: "The account cannot do that on this channel.",
    licence_required: "This search needs a LinkedIn licence the account does not have.",
    search_limit_reached: "This account has run all the searches LinkedIn allows today. More open when the limit resets.",
    search_busy: "LinkedIn is busy with another search on this account. Retrying in a moment.",
    stale_cursor: "These results expired. Run the search again to keep going.",
    recipient_invalid: "That person cannot be messaged from this account.",
    account_unavailable: "There is no account ready to do this. Connect one, or reconnect the one you have, then try again.",
    ai_unavailable: "Describing a search is not available right now. Try again shortly, or set the filters by hand.",
};

// Why a social step was skipped or failed, as GraphKit reports it on the action.
const OUTCOME: Record<string, string> = {
    no_recent_post: "No post recent enough",
    no_post: "No post recent enough",
    not_connected: "Not a connection yet",
    no_skills: "No skills to endorse",
    recipient_unavailable: "Could not reach this person",
    consent_missing: "No consent basis on this channel",
    already_commented: "Already commented on their latest post",
    already_connected: "Already connected",
    invite_pending: "An invite is already pending",
    no_inmail_credits: "No InMail credits left",
    capability_unavailable: "The account cannot do this on this channel",
    consent_required: "No consent basis on this channel",
    recipient_invalid: "Profile not found",
};

/** A skip reason or error code on a social step, in words. Unknown codes are humanised rather than hidden. */
export function socialOutcomeLabel(code: string | null | undefined): string | null {
    if (!code) return null;
    return OUTCOME[code] ?? COPY[code] ?? code.replace(/_/g, " ").replace(/^./, (c) => c.toUpperCase());
}

export function socialErrorCode(err: unknown): string | undefined {
    return (err as Partial<AppError> | null)?.code;
}

/** One friendly sentence for any social error, with the store's copy for payment codes. */
export function socialErrorMessage(err: unknown): string {
    const code = socialErrorCode(err);
    const message = (err as Partial<AppError> | null)?.message;
    // A refused field or a missing capability is named by the server, which says exactly what to fix.
    if ((code === "social_invalid_request" || code === "capability_unavailable") && message) return message;
    if (code && COPY[code]) return COPY[code];
    return storeErrorMessage(err);
}

/** The attempts a sign-in refusal says are left, when it says. */
export function attemptsRemaining(err: unknown): number | null {
    const v = (err as Partial<AppError> | null)?.details?.attempts_remaining;
    return typeof v === "number" ? v : null;
}

/** The GraphKit page a social_checkout_required answer points at. */
export function checkoutUrl(err: unknown): string | null {
    const v = (err as Partial<AppError> | null)?.details?.url;
    return typeof v === "string" && v.startsWith("https://") ? v : null;
}

/** The fields a social_invalid_request names, when it names them. */
export function invalidFields(err: unknown): Record<string, string> | null {
    const v = (err as Partial<AppError> | null)?.details?.fields;
    return v && typeof v === "object" && !Array.isArray(v) ? (v as Record<string, string>) : null;
}

/** The charge a social_accept_required answer asks to accept, in micro-USD. */
export function acceptQuoteMicros(err: unknown): number | null {
    if (socialErrorCode(err) !== "social_accept_required") return null;
    const q = (err as Partial<AppError> | null)?.details?.quote as { monthly_micros?: unknown } | undefined;
    return typeof q?.monthly_micros === "number" ? q.monthly_micros : null;
}
