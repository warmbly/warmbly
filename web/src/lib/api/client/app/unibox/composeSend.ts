// Fresh outbound email from the unibox composer. When email_account_id is
// omitted the backend auto-picks the best sender and reports it back.

import Request from "../../Request";
import type {
    ComposeSendInput,
    ComposeSendResponse,
} from "@/lib/api/models/app/unibox/Compose";

export default async function composeSend(
    data: ComposeSendInput,
): Promise<ComposeSendResponse> {
    return await Request<ComposeSendResponse>({
        method: "POST",
        url: "/unibox/compose",
        data,
        authorization: true,
    });
}
