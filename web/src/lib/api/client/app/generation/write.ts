import type { WriteRequest, WriteResponse } from "@/lib/api/models/app/generation/Write";
import Request from "../../Request";

// POST /generation/write — drafts email copy from a prompt. Returns a 402 when
// the workspace balance is too low; that surfaces as an AppError with status
// 402, and the call site opens the add-money dialog.
export default async function write(body: WriteRequest): Promise<WriteResponse> {
    return await Request<WriteResponse>({
        method: "POST",
        url: "/generation/write",
        data: body,
        authorization: true,
    });
}
