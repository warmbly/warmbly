import type { EditRequest, EditResponse } from "@/lib/api/models/app/generation/Write";
import Request from "../../Request";

// POST /generation/edit — rewrites a selected passage per an instruction.
// Returns a 402 when the org is out of generation credits (AppError status 402
// at the call site).
export default async function edit(body: EditRequest): Promise<EditResponse> {
    return await Request<EditResponse>({
        method: "POST",
        url: "/generation/edit",
        data: body,
        authorization: true,
    });
}
