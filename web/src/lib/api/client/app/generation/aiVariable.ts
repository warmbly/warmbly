import type {
    AIVariableGenerateRequest,
    AIVariableGenerateResponse,
} from "@/lib/api/models/app/generation/AIVariable";
import Request from "../../Request";

// POST /generation/ai-variable — preview one AI-variable snippet for a sample or
// chosen contact. Returns a 402 when the org is out of generation credits.
export default async function generateAIVariable(
    body: AIVariableGenerateRequest,
): Promise<AIVariableGenerateResponse> {
    return await Request<AIVariableGenerateResponse>({
        method: "POST",
        url: "/generation/ai-variable",
        data: body,
        authorization: true,
    });
}
