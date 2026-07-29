import { API_BASE_URL } from "@/lib/information";
import getToken from "@/lib/helper/getToken";
import type { AgentStreamEvent } from "@/lib/api/models/app/agent/Agent";

// streamAgentRun POSTs to an SSE agent endpoint and invokes onEvent for each
// `data:` frame. Uses raw fetch (not the axios client) because the response is
// a streamed body. Aborting the passed signal cancels the run server-side (the
// run executes in the request context), which is the panel's stop button.
export default async function streamAgentRun(
    path: string,
    body: Record<string, unknown>,
    onEvent: (ev: AgentStreamEvent) => void,
    signal?: AbortSignal,
): Promise<void> {
    const token = getToken();
    let res: Response;
    try {
        res = await fetch(`${API_BASE_URL}${path}`, {
            method: "POST",
            headers: {
                "Content-Type": "application/json",
                Accept: "text/event-stream",
                ...(token?.access_token
                    ? { Authorization: `Bearer ${token.access_token}` }
                    : {}),
            },
            body: JSON.stringify(body),
            signal,
        });
    } catch (e) {
        if ((e as Error)?.name === "AbortError") return;
        onEvent({ type: "error", message: "Could not reach the assistant." });
        return;
    }

    // Non-SSE error (auth, not found, service unavailable) comes back as JSON.
    if (!res.ok || !res.body) {
        try {
            const j = await res.json();
            onEvent({
                type: "error",
                code: j.code,
                message: j.message || "The assistant is unavailable.",
            });
        } catch {
            onEvent({ type: "error", message: "The assistant is unavailable." });
        }
        return;
    }

    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";
    try {
        for (;;) {
            const { done, value } = await reader.read();
            if (done) break;
            buffer += decoder.decode(value, { stream: true });
            let sep: number;
            // SSE frames are separated by a blank line.
            while ((sep = buffer.indexOf("\n\n")) >= 0) {
                const frame = buffer.slice(0, sep);
                buffer = buffer.slice(sep + 2);
                const dataLine = frame
                    .split("\n")
                    .find((l) => l.startsWith("data:"));
                if (!dataLine) continue;
                const json = dataLine.slice(5).trim();
                if (!json) continue;
                try {
                    onEvent(JSON.parse(json) as AgentStreamEvent);
                } catch {
                    /* skip malformed frame */
                }
            }
        }
    } catch (e) {
        if ((e as Error)?.name !== "AbortError") {
            onEvent({ type: "error", message: "The connection was interrupted." });
        }
    }
}
