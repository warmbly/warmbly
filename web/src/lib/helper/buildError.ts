import type { AppError } from "../api/client/normalizeError";
import { AuthError } from "../errors/auth";

const PLAIN_CODES = new Set(["portal_session_restricted", "portal_sign_in_elsewhere"]);

// A network failure has no status, and `status && ...` printed the literal
// string "undefined" in front of every one of those messages.
export default function buildError(err: AppError): string {
    // An ended session is not a failure of the action; the sign-in page explains it.
    if ((err as unknown) instanceof AuthError) return err.message;
    // A portal session's limits are not a failure either; the message says where to go instead.
    if (PLAIN_CODES.has(err.code ?? "")) return err.message;
    // Raw Error instances reach this too (a thrown AuthError has no `error`).
    const label = err.error || "Error";
    const prefix = err.status ? `${err.status} ${label}` : label;
    const id = err.request_id ? ` (${err.request_id})` : "";
    return `${prefix}: ${err.message}${id}`;
}
