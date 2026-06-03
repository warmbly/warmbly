import type TemplateScore from "@/lib/api/models/app/campaigns/TemplateScore";
import type { ScoreTemplateRequest } from "@/lib/api/models/app/campaigns/TemplateScore";
import Request from "../../Request";

// Advisory content score for a campaign template. Never blocks — returns a
// 0-100 score (higher = safer) plus a list of non-blocking issues.
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
