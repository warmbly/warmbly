import axios from "axios";
import { AuthError } from "@/lib/errors/auth";

export interface AppError {
    error: string;
    message: string;
    status?: number;
    redirect?: boolean;
    /** Stable machine-readable code from the API, for branching on a specific
     *  condition rather than matching on human-readable text. */
    code?: string;
    /** Optional finer key within one code, where the API offers a different next step. */
    reason?: string;
    /** Correlation id the API already returns, so a user can quote it and an
     *  operator can find the matching server-side log line. */
    request_id?: string;
    /** Where a payment_method_required answer sends the person to save a card. */
    setup_url?: string;
    /** Structured context some codes carry, e.g. the fresh quote on price_changed. */
    details?: Record<string, unknown>;
}

/** True for a request aborted through its AbortSignal (axios CanceledError, ERR_CANCELED). */
export function isRequestCancelled(error: unknown): boolean {
    return axios.isCancel(error) || (error as { code?: unknown } | null)?.code === "ERR_CANCELED";
}

export function normalizeError(error: unknown): AppError {
    if (error instanceof AuthError) {
        return {
            error: "Authentication Required",
            message: error.message,
            status: 401,
            redirect: true,
        };
    }

    if (axios.isAxiosError(error)) {
        if (!error.response) {
            // network, CORS, or timeout
            return {
                error: "Network Error",
                message: "Please check your connection.",
            };
        }

        const status = error.response.status;
        const data = error.response.data;

        if (status === 401) {
            return {
                error: data.error || "Unauthorized",
                message: data.message || "Your session is invalid or expired.",
                status,
                redirect: true,
                code: data.code,
                reason: data.reason,
                request_id: data.request_id,
            };
        }

        return {
            error: data.error || "Unknown Error",
            message: data.message || "Unexpected error occured.",
            status,
            code: data.code,
            reason: data.reason,
            request_id: data.request_id,
            ...(typeof data.setup_url === "string" && data.setup_url ? { setup_url: data.setup_url } : {}),
            ...(data.details && typeof data.details === "object" ? { details: data.details as Record<string, unknown> } : {}),
        }
    }

    return {
        error: "Unknown Error",
        message: "Unexpected error occurred.",
    };
}
