import { API_BASE_URL } from "@/lib/information";
import { ensureValidToken } from "../../Request";
import { readEventStream } from "../../eventStream";
import type { AppError } from "../../normalizeError";
import type { EditRequest, WriteRequest, WriteResponse } from "@/lib/api/models/app/generation/Write";

export interface WritingStreamHandlers {
    // Each fragment, in order, as the model writes it.
    onDelta: (text: string) => void;
    // The text is written and the spam-filter pass is rewording it.
    onChecking?: () => void;
}

type WritingEvent =
    | { type: "delta"; text: string }
    | { type: "stage"; stage: "checking" }
    | ({ type: "done" } & WriteResponse)
    | { type: "error"; code?: string; message?: string };

// streamWriting runs /generation/write or /generation/edit as a Server-Sent
// Events stream and resolves with the final response, whose text can differ
// from the deltas (tidied and reworded after the model finished). It rejects
// with an AppError (status 402 when the balance is too low) or with the AbortError of
// the passed signal.
export default async function streamWriting(
    path: "/generation/write" | "/generation/edit",
    body: WriteRequest | EditRequest,
    handlers: WritingStreamHandlers,
    signal?: AbortSignal,
): Promise<WriteResponse> {
    const token = await ensureValidToken();
    const res = await fetch(`${API_BASE_URL}${path}`, {
        method: "POST",
        headers: {
            "Content-Type": "application/json",
            Accept: "text/event-stream",
            Authorization: `Bearer ${token.access_token}`,
        },
        body: JSON.stringify(body),
        signal,
    }).catch((e: unknown) => {
        if ((e as Error)?.name === "AbortError") throw e;
        throw { error: "Network Error", message: "Please check your connection." } satisfies AppError;
    });

    // Refusals (balance, permissions, limits) come back as a JSON error body.
    if (!res.ok || !res.body) {
        const j = await res.json().catch(() => ({}));
        throw {
            error: j.error || "Unknown Error",
            message: j.message || "The writing assistant is unavailable.",
            status: res.status,
            code: j.code,
            request_id: j.request_id,
        } satisfies AppError;
    }

    let result: WriteResponse | null = null;
    let failure: AppError | null = null;
    await readEventStream(res.body, (data) => {
        const ev = data as WritingEvent;
        if (ev.type === "delta") handlers.onDelta(ev.text);
        else if (ev.type === "stage") handlers.onChecking?.();
        else if (ev.type === "done") result = ev;
        else if (ev.type === "error") {
            // A balance that ran out mid-run reads as the same 402 a refusal is.
            const status = ev.code === "insufficient_credits" ? 402 : 503;
            failure = { error: "Service Unavailable", message: ev.message || "The writing assistant is unavailable.", status, code: ev.code };
        }
    });
    if (failure) throw failure;
    if (!result) throw { error: "Network Error", message: "The connection was interrupted." } satisfies AppError;
    return result;
}
