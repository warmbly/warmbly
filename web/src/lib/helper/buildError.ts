import type { AppError } from "../api/client/normalizeError";

// A network failure has no status, and `status && ...` printed the literal
// string "undefined" in front of every one of those messages.
export default function buildError(err: AppError): string {
    // Raw Error instances reach this too (a thrown AuthError has no `error`).
    const label = err.error || "Error";
    const prefix = err.status ? `${err.status} ${label}` : label;
    const id = err.request_id ? ` (${err.request_id})` : "";
    return `${prefix}: ${err.message}${id}`;
}
