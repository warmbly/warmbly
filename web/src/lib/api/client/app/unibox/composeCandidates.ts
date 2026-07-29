// Sender-picker candidates for a fresh outbound compose. The backend scores
// every connected mailbox against the recipient; the address may be empty
// (backend accepts an empty `to` and skips recipient-specific signals).

import Request from "../../Request";
import type { ComposeCandidatesResponse } from "@/lib/api/models/app/unibox/Compose";

export default async function composeCandidates(
    address = "",
): Promise<ComposeCandidatesResponse> {
    const usp = new URLSearchParams();
    if (address) usp.set("to", address);
    const qs = usp.toString();
    return await Request<ComposeCandidatesResponse>({
        method: "GET",
        url: `/unibox/compose/candidates${qs ? `?${qs}` : ""}`,
        authorization: true,
    });
}
