// Every LinkedIn refusal in Warmbly's own words. Payment codes are the store's and fall through to its copy.
import type { AppError } from "@/lib/api/client/normalizeError";
import { storeErrorMessage } from "@/lib/buy/errors";

const COPY: Record<string, string> = {
    linkedin_unavailable: "LinkedIn is not available on this instance right now. Try again shortly.",
    linkedin_mode_conflict: "This workspace already connects LinkedIn another way. Refresh to see how.",
    linkedin_plan_required: "LinkedIn accounts need a Warmbly plan. Choose a plan, or use your own GraphKit workspace.",
    linkedin_checkout_required: "Your GraphKit plan has no free seat. Add one in GraphKit, then try again.",
    linkedin_sign_in_failed: "LinkedIn did not accept that sign-in. Check the details and try again.",
    linkedin_too_many_sign_ins: "Too many sign-in attempts. Wait a few minutes before trying again.",
    linkedin_session_expired: "This sign-in timed out. Start again.",
    licence_required: "This search needs a LinkedIn licence the account does not have.",
    search_limit_reached: "This account has run all the searches LinkedIn allows today. More open when the limit resets.",
    search_busy: "LinkedIn is busy with another search on this account. Retrying in a moment.",
    stale_cursor: "These results expired. Run the search again to keep going.",
    recipient_invalid: "That person cannot be messaged from this account.",
    account_unavailable: "There is no LinkedIn account ready to do this. Connect one, or reconnect the one you have, then try again.",
    ai_unavailable: "Describing a search is not available right now. Try again shortly, or set the filters by hand.",
    linkedin_unpaid: "This LinkedIn account is paused because its month was not paid. Update your card and it resumes with the next payment.",
    linkedin_invalid_request: "Some of the details were not accepted. Check them and try again.",
    linkedin_acknowledgement_required: "Tick the box that you accept the risk of automating this account, then continue.",
    linkedin_accept_required: "Connecting this account is charged. Accept the charge to continue.",
    invalid_linkedin_url: "That is not a LinkedIn profile address. Use one like linkedin.com/in/name.",
};

export function linkedInErrorCode(err: unknown): string | undefined {
    return (err as Partial<AppError> | null)?.code;
}

/** One friendly sentence for any LinkedIn error, with the store's copy for payment codes. */
export function linkedInErrorMessage(err: unknown): string {
    const code = linkedInErrorCode(err);
    // A refused field is named by the server, which says exactly what to fix.
    const message = (err as Partial<AppError> | null)?.message;
    if (code === "linkedin_invalid_request" && message) return message;
    if (code && COPY[code]) return COPY[code];
    return storeErrorMessage(err);
}

/** The attempts a sign-in refusal says are left, when it says. */
export function attemptsRemaining(err: unknown): number | null {
    const v = (err as Partial<AppError> | null)?.details?.attempts_remaining;
    return typeof v === "number" ? v : null;
}

/** The GraphKit page a linkedin_checkout_required answer points at. */
export function checkoutUrl(err: unknown): string | null {
    const v = (err as Partial<AppError> | null)?.details?.url;
    return typeof v === "string" && v.startsWith("https://") ? v : null;
}

/** The fields a linkedin_invalid_request names, when it names them. */
export function invalidFields(err: unknown): Record<string, string> | null {
    const v = (err as Partial<AppError> | null)?.details?.fields;
    return v && typeof v === "object" && !Array.isArray(v) ? (v as Record<string, string>) : null;
}

/** The charge a linkedin_accept_required answer asks to accept, in micro-USD. */
export function acceptQuoteMicros(err: unknown): number | null {
    if (linkedInErrorCode(err) !== "linkedin_accept_required") return null;
    const q = (err as Partial<AppError> | null)?.details?.quote as { monthly_micros?: unknown } | undefined;
    return typeof q?.monthly_micros === "number" ? q.monthly_micros : null;
}
