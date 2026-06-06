import type { WriteRequest, WriteResponse } from "@/lib/api/models/app/generation/Write";
import Request from "../../Request";

// POST /generation/write — drafts email copy from a prompt. Returns a 402 when
// the org is out of generation credits; that surfaces as an AppError with
// status 402 (handled at the call site with a friendly toast).
export default async function write(body: WriteRequest): Promise<WriteResponse> {
    return await Request<WriteResponse>({
        method: "POST",
        url: "/generation/write",
        data: body,
        authorization: true,
    });
}
