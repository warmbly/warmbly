import type TemplateScore from "@/lib/api/models/app/campaigns/TemplateScore";
import type { ScoreTemplateRequest } from "@/lib/api/models/app/campaigns/TemplateScore";
import Request from "../../Request";

// Returns the live score and whether the pre-send guardrail considers it hard.
export default async function scoreTemplate(
    body: ScoreTemplateRequest,
): Promise<TemplateScore> {
    return await Request<TemplateScore>({
        method: "POST",
        url: "/templates/score",
        data: body,
        authorization: true,
    });
}
