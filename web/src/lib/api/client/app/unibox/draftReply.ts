import Request from "../../Request";

export interface DraftReplyInput {
    thread_id: string;
    instruction?: string;
    // Per-attempt key so a network retry of the SAME draft is never charged
    // twice (the backend keys the charge on it).
    idempotency_key?: string;
}

export interface DraftReplyResult {
    text: string;
    // What the draft cost in dollars, and the balance after it.
    charged_usd: number;
    balance_usd: number;
    tokens_used: number;
    model: string;
}

// Context-grounded AI reply draft. Returns a draft the human reviews and sends;
// it never sends anything.
export default async function draftReply(
    data: DraftReplyInput,
): Promise<DraftReplyResult> {
    const { idempotency_key, ...body } = data;
    return await Request<DraftReplyResult>({
        method: "POST",
        url: `/unibox/reply/draft`,
        data: body,
        authorization: true,
        headers: idempotency_key ? { "Idempotency-Key": idempotency_key } : undefined,
    });
}
