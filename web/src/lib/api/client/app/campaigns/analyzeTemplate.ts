import type {
    ScoreTemplateRequest,
    TemplateAnalysis,
} from "@/lib/api/models/app/campaigns/TemplateScore";
import Request from "../../Request";

// AI spam analysis of a campaign template: the rules score plus the specific
// words and sentences a filter objects to, where they are, and what to write
// instead. Charged to the balance, so it only ever runs on an explicit click.
export default async function analyzeTemplate(
    body: ScoreTemplateRequest,
): Promise<TemplateAnalysis> {
    return await Request<TemplateAnalysis>({
        method: "POST",
        url: "/templates/analyze",
        data: body,
        authorization: true,
    });
}
