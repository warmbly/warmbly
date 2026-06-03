import { useMutation } from "@tanstack/react-query";
import scoreTemplate from "@/lib/api/client/app/campaigns/scoreTemplate";
import type { ScoreTemplateRequest } from "@/lib/api/models/app/campaigns/TemplateScore";

// On-demand advisory content score for a campaign template. A mutation rather
// than a query because it's run explicitly via a "Check content" button, not
// on every keystroke.
export default function useScoreTemplate() {
    return useMutation({
        mutationFn: (body: ScoreTemplateRequest) => scoreTemplate(body),
    });
}
